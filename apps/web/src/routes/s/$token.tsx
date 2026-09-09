import { createFileRoute, useParams } from "@tanstack/react-router";
import { Button } from "@workspace/ui/components/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { fmtSize } from "@/lib/api";
import { shareInfoOptions } from "@/lib/queries";

export const Route = createFileRoute("/s/$token")({
  head: () => ({ meta: [{ title: "Shared file · pp-hub" }] }),
  component: ShareLanding,
});

function ShareLanding() {
  const { token } = useParams({ from: "/s/$token" });
  const info = shareInfoOptions(token);

  if (info.isPending) {
    return (
      <main className="bg-background flex min-h-svh items-center justify-center p-6">
        <p className="text-muted-foreground text-sm">Loading…</p>
      </main>
    );
  }

  if (info.isError) {
    return (
      <main className="bg-background flex min-h-svh items-center justify-center p-6">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle className="text-lg">pp-hub</CardTitle>
            <CardDescription>Invalid or expired share link.</CardDescription>
          </CardHeader>
        </Card>
      </main>
    );
  }

  const { node, perm } = info.data;
  const canDownload = perm === "download" || perm === "edit";

  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">pp-hub</CardTitle>
          <CardDescription>
            Shared {node.kind}: <strong>{node.name}</strong>
            {node.size != null ? ` (${fmtSize(node.size)})` : ""}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <p className="text-muted-foreground text-sm">Permission: {perm}</p>
          {canDownload ? (
            <Button render={<a href={`/s/${token}/download`} />}>Download</Button>
          ) : null}
        </CardContent>
      </Card>
    </main>
  );
}
