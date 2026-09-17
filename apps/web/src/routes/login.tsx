import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { Button } from "@workspace/ui/components/button";
import { fetchMe } from "@/lib/queries";

export const Route = createFileRoute("/login")({
  head: () => ({ meta: [{ title: "Sign in · pp-hub" }] }),
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    // Only a same-origin path is honoured: an absolute URL here would make the
    // sign-in page an open redirect.
    redirect:
      typeof search.redirect === "string" && search.redirect.startsWith("/")
        ? search.redirect
        : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const me = await fetchMe(context.queryClient);
    if (me) throw redirect({ href: search.redirect ?? "/drive" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { redirect: target } = Route.useSearch();

  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">pp-hub</CardTitle>
          <CardDescription>Minimal self-hosted workspace: drive, office, spaces.</CardDescription>
        </CardHeader>
        <CardContent>
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
