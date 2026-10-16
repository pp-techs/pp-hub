import { useQuery } from "@tanstack/react-query";
import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Puzzle } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { Skeleton } from "@workspace/ui/components/skeleton";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import {
  createEditSession,
  isMarkdownFile,
  isOfficeFile,
  type EditSession,
  type Node,
} from "@/lib/api";
import { nodeOptions } from "@/lib/queries";
import type { FileSearch } from "@/lib/open-file";
import { faviconFor } from "@/components/node-icon";
import { useDocumentTitle, useFavicon } from "@/lib/use-page-head";
import { useLocale } from "@/components/locale-provider";
import { useTheme } from "@/components/theme-provider";
import { LoadingStatus } from "@/components/loading";
import { extKindOf } from "@/ext/registry";
import { OnScreen, useOnScreen } from "@/components/header-slot";
import { EditorHeader } from "@/components/editor-header";
import { DocEditor } from "@/components/doc-editor/doc-editor";
import { PreviewContent } from "@/components/preview-content";

/**
 * An open file in the drive shell's content column. Which view is a question
 * of type: a markdown document opens in the block editor, an office file in
 * OnlyOffice (PDFs too: its PDF editor), an extension node in its app's
 * editor, anything else in the viewer the preview uses (image, video, audio, text). The header around each
 * is the shell's, filled through `EditorHeader`.
 *
 * `nodeId` may change under a mounted view — following a link from one
 * markdown page to another. That does not tear the screen down: the page on
 * screen stays, interactive, while the next one loads its node, joins its
 * room and builds its editor out of sight, and the two swap in one frame. The
 * kit latches a page's room, Y.Doc and options once, so the next page is a
 * second editor beside the first rather than the first retargeted. Anything
 * else — a page that fails, an office file, a page the screen was still
 * waiting on — takes the screen at once.
 */
export function FileView({ nodeId, thread, subject }: { nodeId: string } & FileSearch) {
  const [shown, setShown] = useState(nodeId);
  // The page whose editor (or refusal) has been drawn: only a drawn page is
  // worth holding on screen.
  const [settled, setSettled] = useState<string | null>(null);
  const target = useFileNode(nodeId);
  const current = useFileNode(shown);
  const onScreen = useOnScreen();

  if (shown !== nodeId) {
    const hold =
      settled === shown &&
      !!current.data &&
      isMarkdownFile(current.data) &&
      !target.error &&
      (!target.data || isMarkdownFile(target.data));
    if (!hold) setShown(nodeId);
  }
  // Mounted once its type is known: only a markdown page waits behind another.
  const incoming = shown !== nodeId && target.data && isMarkdownFile(target.data) ? nodeId : null;

  const settle = useCallback((id: string) => {
    setSettled(id);
    // Only the incoming page is ever mounted besides the shown one, so a
    // settled page that is not the shown one is the one to show.
    setShown(id);
  }, []);

  // Only the file on screen names the browser tab.
  useDocumentTitle(onScreen ? current.data?.name : undefined);
  useFavicon(onScreen && current.data ? faviconFor(current.data.name) : undefined);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {(incoming ? [shown, incoming] : [shown]).map((id) => (
        <OnScreen key={id} value={id === shown}>
          <FileBody
            nodeId={id}
            visible={id === shown}
            // A notification's thread belongs to the page the URL names.
            thread={id === nodeId ? thread : undefined}
            subject={id === nodeId ? subject : undefined}
            onSettled={settle}
          />
        </OnScreen>
      ))}
    </div>
  );
}

/**
 * A markdown document's versions are written by the server when the room
 * goes quiet, with no event to the page, so the header's "version n" is
 * refreshed on a timer instead of going stale for the whole session.
 */
function useFileNode(nodeId: string) {
  return useQuery({
    ...nodeOptions(nodeId),
    refetchInterval: (query) =>
      query.state.data && isMarkdownFile(query.state.data) ? 30_000 : false,
  });
}

/** One page's header and view; out of sight (`visible` off) while it loads behind the shown one. */
function FileBody({
  nodeId,
  visible,
  thread,
  subject,
  onSettled,
}: {
  nodeId: string;
  visible: boolean;
  thread?: string;
  subject?: string;
  onSettled: (nodeId: string) => void;
}) {
  const node = useFileNode(nodeId);
  const settled = useCallback(() => onSettled(nodeId), [onSettled, nodeId]);

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col", !visible && "hidden")}>
      {!node.data ? (
        <>
          <EditorHeader nodeId={nodeId} node={undefined} />
          {node.error ? (
            <p className="text-destructive flex flex-1 items-center justify-center text-sm">
              {node.error.message}
            </p>
          ) : (
            <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
              <Trans>Opening editor…</Trans>
            </p>
          )}
        </>
      ) : node.data.kind === "ext" ? (
        <ExtEditor node={node.data} subject={subject} thread={thread} />
      ) : isMarkdownFile(node.data) ? (
        <DocEditor node={node.data} thread={thread} onSettled={settled} />
      ) : isOfficeFile(node.data.name) ? (
        <OfficeEditor node={node.data} />
      ) : (
        <>
          <EditorHeader nodeId={node.data.id} node={node.data} />
          <PreviewContent node={node.data} />
        </>
      )}
    </div>
  );
}

/**
 * An extension node: the editor its extension registered for the node's kind,
 * under the same header as the other editors. A kind this build does not carry
 * (the server knows it, the SPA does not) has nothing to open it with. A board
 * the app switched off answers 403 to the editor's own requests, and the
 * editor says so itself.
 */
function ExtEditor({ node, subject, thread }: { node: Node; subject?: string; thread?: string }) {
  const kind = extKindOf(node.ext_kind);
  const Editor = kind?.editor;
  return (
    <>
      <EditorHeader nodeId={node.id} node={node} />
      {Editor ? (
        <Suspense fallback={<ExtSkeleton />}>
          <Editor nodeId={node.id} subject={subject} thread={thread} />
        </Suspense>
      ) : (
        <Empty className="flex-1">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Puzzle />
            </EmptyMedia>
            <EmptyTitle>
              <Trans>This app is not part of this installation</Trans>
            </EmptyTitle>
            <EmptyDescription>
              <Trans>
                This item belongs to an app that this version of PP Hub does not include. It can
                still be renamed, moved or deleted from the drive.
              </Trans>
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </>
  );
}

/** Stand-in while an extension's editor chunk loads: a row of column-shaped blocks. */
function ExtSkeleton() {
  return (
    <LoadingStatus className="flex min-h-0 flex-1 gap-3 overflow-hidden p-4">
      {[0, 1, 2].map((i) => (
        <Skeleton key={i} className="h-2/3 w-72 shrink-0 rounded-lg" />
      ))}
    </LoadingStatus>
  );
}

/**
 * OnlyOffice in an iframe. Everything inside it — the ribbon, the save
 * indicator, the collaborator carets and presence list — belongs to
 * OnlyOffice and is deliberately not duplicated in the header.
 */
function OfficeEditor({ node }: { node: Node }) {
  const { t } = useLingui();
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const [session, setSession] = useState<EditSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    // The Document Server takes its ribbon language from `ui` and its UI
    // theme from `thm`, so the session is minted for the language and theme
    // the app is in right now. Changing either mints a new one, and the
    // frame below remounts on the new token so the editor actually comes
    // back in it. resolvedTheme, not theme: "system" means nothing to a
    // process that is not this browser.
    createEditSession(node.id, locale, resolvedTheme)
      .then((s) => {
        if (!cancelled) setSession(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [node.id, locale, resolvedTheme]);

  return (
    <>
      <EditorHeader
        nodeId={node.id}
        node={node}
        // A reader gets the document, not the editor, and the ribbon alone
        // is a subtle thing to read that from.
        status={session && !session.can_write ? t`read-only` : t`saved by OnlyOffice`}
      />
      {error ? (
        <p className="text-destructive flex flex-1 items-center justify-center text-sm">{error}</p>
      ) : !session ? (
        <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
          <Trans>Opening editor…</Trans>
        </p>
      ) : (
        <EditorFrame key={session.token} nodeId={node.id} session={session} />
      )}
    </>
  );
}

/**
 * Posts the WOPI form once on mount; the iframe (its target) renders the
 * editor. The frame is named per file: several open files are several
 * frames in one document, and a form posts into the first frame its target
 * names.
 */
function EditorFrame({ nodeId, session }: { nodeId: string; session: EditSession }) {
  const { t } = useLingui();
  const formRef = useRef<HTMLFormElement>(null);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- iframe/form target name
  const frame = `pp-office-frame-${nodeId}`;

  useEffect(() => {
    formRef.current?.submit();
  }, []);

  return (
    <>
      <form
        ref={formRef}
        action={session.editor_url}
        method="post"
        target={frame}
        className="hidden"
      >
        <input type="hidden" name="access_token" value={session.token} />
        <input type="hidden" name="access_token_ttl" value={session.access_token_ttl} />
      </form>
      <iframe
        name={frame}
        title={t`OnlyOffice editor`}
        className="min-h-0 flex-1 border-0"
        allow="clipboard-write"
        allowFullScreen
      />
    </>
  );
}
