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
  beforeLoad: async ({ context }) => {
    const me = await fetchMe(context.queryClient);
    if (me) throw redirect({ to: "/drive" });
  },
  component: LoginPage,
});

function LoginPage() {
  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">pp-hub</CardTitle>
          <CardDescription>Minimal self-hosted workspace: drive, office, spaces.</CardDescription>
        </CardHeader>
        <CardContent>
          {/* Server-side OIDC handshake; callback sets the session cookie and
              redirects to "/" — no tokens in the SPA. */}
          <Button className="w-full" render={<a href="/auth/login" />}>
            Sign in
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}
