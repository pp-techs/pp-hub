import { useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { CircleAlert, RotateCw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { appRetry, ExtUnavailable, isAppOff } from "./ext-unavailable";
import type { ExtEditorProps } from "./types";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@workspace/ui/components/sheet";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { ThreadList } from "@/components/comments/thread-list";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { LoadingStatus } from "@/components/loading";
import { capsOf, type Extension } from "@/lib/api";
import { extensionName } from "@/lib/ext-name";
import { useMe, useNode } from "@/lib/queries";
import { extKindOf, useExtRegistry } from "./registry";
import { attachFrame, probeApp, SANDBOX, type ConfirmRequest, type HostState } from "./app-host";

// A packaged app's editor (M51): its own page in a sandboxed frame, served
// from the app asset origin under a CSP that gives it an opaque origin and
// no network but what its manifest declares. Everything it does in pp-hub it
// asks this page for, over the bridge (app-host.ts).

type Phase = "loading" | "ready" | "left" | "silent";

type PendingConfirm = ConfirmRequest & { resolve: (ok: boolean) => void };

export function AppFrame({ app, nodeId, subject, thread }: { app: Extension } & ExtEditorProps) {
  const { t, i18n } = useLingui();
  const node = useNode(nodeId);
  const me = useMe();
  const [generation, setGeneration] = useState(0);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- phase name
  const [phase, setPhase] = useState<Phase>("loading");
  // A comment notification opens the discussion it is about.
  const [comments, setComments] = useState<{ subject: string; thread?: string } | null>(
    subject && thread ? { subject, thread } : null,
  );
  const [confirm, setConfirm] = useState<PendingConfirm | null>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const name = extensionName(app, i18n);
  // Whether this person may open the app on this item at all, asked before
  // any of the app's code loads: an app that is off says so here, in pp-hub's
  // words, rather than in whatever the app makes of a 403.
  const probe = useQuery({
    queryKey: ["app-probe", app.id, nodeId, generation],
    queryFn: () => probeApp(app.id, nodeId),
    retry: appRetry,
    // Asked afresh on every opening: a switch flipped since must count.
    gcTime: 0,
  });
  useExtRegistry();
  const kind = extKindOf(node.data?.ext_kind);
  const what = kind ? i18n._(kind.label).toLowerCase() : t`item`;

  // What the bridge reads when the frame asks: the latest of each, without
  // reattaching the frame when one changes.
  const state: HostState = {
    node: node.data,
    me: me.data,
    locale: i18n.locale,
    subject,
    thread,
    name,
  };
  const latest = useRef(state);
  useEffect(() => {
    latest.current = state;
  });

  /* eslint-disable lingui/no-unlocalized-strings -- phase names */
  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;
    return attachFrame(frame, app, nodeId, {
      state: () => latest.current,
      ready: () => setPhase("ready"),
      silent: () => setPhase((p) => (p === "loading" ? "silent" : p)),
      left: () => setPhase("left"),
      openComments: (s, th) => setComments({ subject: s, thread: th }),
      // The build targets ES2022, which has no Promise.withResolvers.
      confirm: (req) => new Promise<boolean>((resolve) => setConfirm({ ...req, resolve })),
    });
  }, [app, nodeId, generation, probe.isSuccess]);

  const reload = () => {
    setPhase("loading");
    setGeneration((g) => g + 1);
  };
  /* eslint-enable lingui/no-unlocalized-strings */
  const caps = capsOf(node.data);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {probe.isError ? (
        <ExtUnavailable
          what={what}
          off={isAppOff(probe.error)}
          onRetry={() => void probe.refetch()}
        />
      ) : phase === "left" || phase === "silent" ? (
        <Empty className="flex-1">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <CircleAlert />
            </EmptyMedia>
            <EmptyTitle>
              {phase === "left" ? (
                <Trans>{name} left its page</Trans>
              ) : (
                <Trans>{name} did not start</Trans>
              )}
            </EmptyTitle>
            <EmptyDescription>
              {phase === "left" ? (
                <Trans>
                  The app opened another page, which cannot reach your files. Reload it to go back.
                </Trans>
              ) : (
                <Trans>
                  The app loaded but never connected to PP Hub. It may be broken or built for a
                  different version; an administrator can check it in Admin › Apps.
                </Trans>
              )}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" onClick={reload}>
              <RotateCw data-icon="inline-start" />
              <Trans>Reload</Trans>
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <>
          {phase === "loading" && (
            <LoadingStatus className="bg-background absolute inset-0 flex flex-col gap-3 p-4">
              <Skeleton className="h-8 w-1/3" />
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </LoadingStatus>
          )}
          {probe.isSuccess && (
            <iframe
              key={generation}
              ref={frameRef}
              src={app.package?.entry_url}
              title={name}
              sandbox={SANDBOX}
              referrerPolicy="no-referrer"
              className="min-h-0 w-full flex-1 border-0"
            />
          )}
        </>
      )}

      <Sheet open={comments !== null} onOpenChange={(open) => !open && setComments(null)}>
        <SheetContent className="flex w-full flex-col gap-0 sm:max-w-md">
          <SheetHeader>
            <SheetTitle>
              <Trans>Discussion</Trans>
            </SheetTitle>
            <SheetDescription>{node.data?.name}</SheetDescription>
          </SheetHeader>
          {comments && (
            <ThreadList
              nodeId={nodeId}
              subject={comments.subject}
              canComment={caps.view}
              canResolve={caps.edit}
              focusThreadId={comments.thread}
              className="min-h-0 flex-1 overflow-y-auto px-4 pb-4"
            />
          )}
        </SheetContent>
      </Sheet>

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!open && confirm) {
            confirm.resolve(false);
            setConfirm(null);
          }
        }}
        title={confirm?.title ?? ""}
        description={confirm?.message ?? ""}
        confirmLabel={confirm?.confirmLabel ?? t`Continue`}
        confirmVariant={confirm?.destructive ? "destructive" : "default"}
        onConfirm={() => {
          confirm?.resolve(true);
          setConfirm(null);
        }}
      />
    </div>
  );
}
