import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Download, History, Link2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { createEditSession, fmtSize, type EditSession } from "@/lib/api";
import { requireAuth, useNode } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { NodePanels } from "@/components/node-panels";
import { NodeIcon } from "@/components/node-icon";
import { ShareDialog } from "@/components/share-dialog";

export const Route = createFileRoute("/edit/$nodeId")({
  head: () => ({ meta: [{ title: "Editing · pp-hub" }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
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
  const { nodeId } = Route.useParams();
  const node = useNode(nodeId);
  const openPanel = useUiStore((s) => s.openPanel);
  const [session, setSession] = useState<EditSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    createEditSession(nodeId)
      .then((s) => {
        if (!cancelled) setSession(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [nodeId]);

  return (
    <div className="bg-background text-foreground flex h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Back to Drive"
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
          <span className="truncate text-sm font-medium">{node.data?.name ?? "Loading…"}</span>
          <span className="text-muted-foreground truncate text-[10px] tabular-nums">
            {node.data
              ? [
                  node.data.version ? `version ${node.data.version}` : null,
                  node.data.size != null ? fmtSize(node.data.size) : null,
                  "saved by OnlyOffice",
                ]
                  .filter(Boolean)
                  .join(" · ")
              : ""}
          </span>
        </div>

        <div className="ml-auto flex shrink-0 items-center gap-1">
          <Button
            variant="outline"
            size="sm"
            disabled={!node.data}
            onClick={() => node.data && void ShareDialog.show({ node: node.data })}
          >
            <Link2 data-icon="inline-start" /> Share
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={!node.data}
            onClick={() => node.data && openPanel("versions", node.data)}
          >
            <History data-icon="inline-start" /> Versions
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Download"
            render={<a href={`/api/v1/nodes/${nodeId}/download`} />}
          >
            <Download />
          </Button>
        </div>
      </header>

      {error ? (
        <p className="text-destructive flex flex-1 items-center justify-center text-sm">{error}</p>
      ) : !session ? (
        <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
          Opening editor…
        </p>
      ) : (
        <EditorFrame session={session} />
      )}

      <NodePanels />
    </div>
  );
}

/** Posts the WOPI form once on mount; the iframe (its target) renders the editor. */
function EditorFrame({ session }: { session: EditSession }) {
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
        title="OnlyOffice editor"
        className="min-h-0 flex-1 border-0"
        allow="clipboard-write"
        allowFullScreen
      />
    </>
  );
}
