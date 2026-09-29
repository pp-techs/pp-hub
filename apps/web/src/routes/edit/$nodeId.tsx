import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { createEditSession, isMarkdownFile, type EditSession, type Node } from "@/lib/api";
import { nodeOptions, requireAuth, useNode } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { useLocale } from "@/components/locale-provider";
import { useTheme } from "@/components/theme-provider";
import { NodePanels } from "@/components/node-panels";
import { EditorHeader } from "@/components/editor-header";
import { DocEditor } from "@/components/doc-editor/doc-editor";

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
 * Full-page editor, opened in a new tab from the drive. Which editor is a
 * question of type: a markdown document opens in the block editor, anything
 * else in OnlyOffice. The header around either is ours.
 */
function EditPage() {
  const { nodeId } = Route.useParams();
  const node = useNode(nodeId);

  return (
    <div className="bg-background text-foreground flex h-svh flex-col">
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
      ) : isMarkdownFile(node.data) ? (
        <DocEditor node={node.data} />
      ) : (
        <OfficeEditor node={node.data} />
      )}
      <NodePanels />
    </div>
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
        <EditorFrame key={session.token} session={session} />
      )}
    </>
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
