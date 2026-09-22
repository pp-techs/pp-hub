import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { ArrowLeft, Download, History, Link2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { capsOf, createEditSession, type EditSession } from "@/lib/api";
import { nodeOptions, requireAuth, useNode } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useFormatters } from "@/lib/format";
import { pageTitle } from "@/lib/i18n";
import { useLocale } from "@/components/locale-provider";
import { useTheme } from "@/components/theme-provider";
import { NodePanels } from "@/components/node-panels";
import { NodeIcon } from "@/components/node-icon";
import { ShareDialog } from "@/components/share-dialog";

export const Route = createFileRoute("/edit/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Editing`) }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(nodeOptions(params.nodeId));
  },
  component: EditPage,
});

/**
 * Full-page OnlyOffice editor, opened in a new tab from the drive.
 *
 * The chrome around the iframe is ours; everything inside it — the ribbon,
 * the save indicator, the collaborator carets and presence list — belongs to
 * OnlyOffice and is deliberately not duplicated here. What the app adds is the
 * things OnlyOffice cannot know about: where the file lives, and the pp-hub
 * actions (share, version history, download) that operate on the node.
 */
function EditPage() {
  const { t } = useLingui();
  const f = useFormatters();
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const { nodeId } = Route.useParams();
  const node = useNode(nodeId);
  const openPanel = useUiStore((s) => s.openPanel);
  const [session, setSession] = useState<EditSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Named so the extracted message reads `version {version}` rather than
  // `version {0}`: the placeholder name is all a translator gets.
  const version = node.data?.version;

  useEffect(() => {
    let cancelled = false;
    // The Document Server takes its ribbon language from `ui` and its UI
    // theme from `thm`, so the session is minted for the language and theme
    // the app is in right now. Changing either mints a new one, and the
    // frame below remounts on the new token so the editor actually comes
    // back in it. resolvedTheme, not theme: "system" means nothing to a
    // process that is not this browser.
    createEditSession(nodeId, locale, resolvedTheme)
      .then((s) => {
        if (!cancelled) setSession(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [nodeId, locale, resolvedTheme]);

  return (
    <div className="bg-background text-foreground flex h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t`Back to Drive`}
          render={
            <Link
              to={node.data ? ("/drive/$spaceId" as const) : ("/drive" as const)}
              params={node.data ? { spaceId: node.data.space_id } : undefined}
              search={node.data ? { dir: node.data.parent_id } : undefined}
            />
          }
        >
          <ArrowLeft />
        </Button>

        {node.data && <NodeIcon node={node.data} className="size-4 shrink-0" />}
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm font-medium">{node.data?.name ?? t`Loading…`}</span>
          <span className="text-muted-foreground truncate text-[10px] tabular-nums">
            {node.data
              ? [
                  version ? t`version ${version}` : null,
                  node.data.size != null ? f.size(node.data.size) : null,
                  // A reader gets the document, not the editor, and the
                  // ribbon alone is a subtle thing to read that from.
                  session && !session.can_write ? t`read-only` : t`saved by OnlyOffice`,
                ]
                  .filter(Boolean)
                  .join(" · ")
              : ""}
          </span>
        </div>

        <div className="ml-auto flex shrink-0 items-center gap-1">
          {node.data && capsOf(node.data).share && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => node.data && void ShareDialog.show({ node: node.data })}
            >
              <Link2 data-icon="inline-start" /> <Trans>Share</Trans>
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            disabled={!node.data}
            onClick={() => node.data && openPanel("versions", node.data)}
          >
            <History data-icon="inline-start" /> <Trans>Versions</Trans>
          </Button>
          {node.data && capsOf(node.data).download && (
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t`Download`}
              render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
            >
              <Download />
            </Button>
          )}
        </div>
      </header>

      {error ? (
        <p className="text-destructive flex flex-1 items-center justify-center text-sm">{error}</p>
      ) : !session ? (
        <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
          <Trans>Opening editor…</Trans>
        </p>
      ) : (
        <EditorFrame key={session.token} session={session} />
      )}

      <NodePanels />
    </div>
  );
}

/** Posts the WOPI form once on mount; the iframe (its target) renders the editor. */
function EditorFrame({ session }: { session: EditSession }) {
  const { t } = useLingui();
  const formRef = useRef<HTMLFormElement>(null);

  useEffect(() => {
    formRef.current?.submit();
  }, []);

  return (
    <>
      <form
        ref={formRef}
        action={session.editor_url}
        method="post"
        target="pp-office-frame"
        className="hidden"
      >
        <input type="hidden" name="access_token" value={session.token} />
        <input type="hidden" name="access_token_ttl" value={session.access_token_ttl} />
      </form>
      <iframe
        name="pp-office-frame"
        title={t`OnlyOffice editor`}
        className="min-h-0 flex-1 border-0"
        allow="clipboard-write"
        allowFullScreen
      />
    </>
  );
}
