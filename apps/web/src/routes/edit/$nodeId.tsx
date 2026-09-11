import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { Button } from "@workspace/ui/components/button";
import { createEditSession, type EditSession } from "@/lib/api";
import { requireAuth } from "@/lib/queries";

export const Route = createFileRoute("/edit/$nodeId")({
  head: () => ({ meta: [{ title: "Editing · pp-hub" }] }),
  beforeLoad: async ({ context }) => {
    await requireAuth(context.queryClient);
  },
  component: EditPage,
});

/**
 * Full-page OnlyOffice editor. Opened in a new tab from the drive; the server
 * edit-session returns the 9.x action URL + token, and we POST them into a
 * full-viewport iframe (OnlyOffice 9.x WOPI requires a form POST, not a GET).
 */
function EditPage() {
  const { nodeId } = Route.useParams();
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
      <header className="flex items-center justify-between gap-4 border-b px-4 py-2">
        <p className="text-muted-foreground truncate text-sm">
          Editing in OnlyOffice — close this tab when you are done.
        </p>
        <Link to="/drive">
          <Button variant="outline" size="sm">
            Back to Drive
          </Button>
        </Link>
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
