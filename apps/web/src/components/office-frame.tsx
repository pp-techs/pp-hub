import { useEffect, useRef, useState } from "react";
import { useLingui } from "@lingui/react/macro";
import { createEditSession, type EditSession } from "@/lib/api";
import { useLocale } from "@/components/locale-provider";
import { useTheme } from "@/components/theme-provider";

/**
 * An OnlyOffice session for `nodeId`, minted for the language and theme the
 * app is in right now. The Document Server takes its ribbon language from
 * `ui` and its UI theme from `thm`; changing either mints a new session, and
 * the frame remounts on the new token so the editor comes back in it.
 * resolvedTheme, not theme: "system" means nothing to a process that is not
 * this browser.
 */
export function useOfficeSession(nodeId: string): {
  session: EditSession | null;
  error: string | null;
} {
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const [session, setSession] = useState<EditSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
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

  return { session, error };
}

/**
 * Posts the WOPI form once on mount; the iframe (its target) renders the
 * editor. The frame is named per file: several open files are several
 * frames in one document, and a form posts into the first frame its target
 * names.
 */
export function EditorFrame({ nodeId, session }: { nodeId: string; session: EditSession }) {
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
