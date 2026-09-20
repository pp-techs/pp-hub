import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { Alert, AlertDescription, AlertTitle } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import { fetchMe } from "@/lib/queries";
import { safePath } from "@/lib/safe-path";

/**
 * Why a sign-in attempt came back here.
 *
 * `/auth/callback` is a top-level navigation, so it cannot answer with the
 * problem+json the rest of the server uses — that would render as raw JSON in
 * front of somebody who only tried to log in. It logs the cause and redirects
 * with a code instead, and this is where the code becomes a sentence.
 * Anything unrecognised is an OAuth error parameter passed through by the
 * provider.
 */
const ERRORS: Record<string, string> = {
  state: "That sign-in link expired or was already used. Start again from this page.",
  exchange: "The identity provider refused to complete the sign-in. Try again.",
  token: "The identity provider's response could not be verified. Try again.",
  access_denied: "Sign-in was cancelled.",
  suspended: "This account is suspended. Contact an administrator.",
  "email-taken":
    "An account already exists with this email address under a different identity provider. Contact an administrator.",
  internal: "Something went wrong on our side. Try again in a moment.",
};

export const Route = createFileRoute("/login")({
  head: () => ({ meta: [{ title: "Sign in · pp-hub" }] }),
  validateSearch: (search: Record<string, unknown>): { redirect?: string; error?: string } => ({
    // Only a same-origin path is honoured: an absolute URL — or the
    // protocol-relative shapes a bare "/" prefix test lets through — would
    // make the sign-in page an open redirect. `safePath` is the same rule the
    // server applies to `next`.
    redirect: safePath(search.redirect),
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const me = await fetchMe(context.queryClient);
    if (me) throw redirect({ href: search.redirect ?? "/drive" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { redirect: target, error } = Route.useSearch();

  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">pp-hub</CardTitle>
          <CardDescription>Minimal self-hosted workspace: drive, office, spaces.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {error && (
            <Alert variant="destructive">
              <AlertTitle>Could not sign you in</AlertTitle>
              <AlertDescription>{ERRORS[error] ?? "Sign-in failed. Try again."}</AlertDescription>
            </Alert>
          )}
          {/* Server-side OIDC handshake; no tokens in the SPA. `next` survives
              the provider round trip in its own short-lived cookie, so the
              callback lands back on the page the caller was bounced from
              rather than always on "/". */}
          <Button
            className="w-full"
            render={
              <a href={target ? `/auth/login?next=${encodeURIComponent(target)}` : "/auth/login"} />
            }
          >
            Sign in
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}
