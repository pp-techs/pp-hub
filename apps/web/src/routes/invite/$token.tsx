import { createFileRoute, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Trans } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { Check, Mail } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { acceptInvitation } from "@/lib/api";
import { requireAuth } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";

export const Route = createFileRoute("/invite/$token")({
  head: () => ({ meta: [{ title: pageTitle(msg`Invitation`) }] }),
  // Redeeming names an account, not an address, so sign-in comes first. The
  // guard bounces to login and back, landing here with a session.
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  component: AcceptInvite,
});

function AcceptInvite() {
  const { token } = useParams({ from: "/invite/$token" });
  const navigate = useNavigate();
  const qc = useQueryClient();

  const accept = useMutation({
    mutationFn: () => acceptInvitation(token),
    onSuccess: (space) => {
      void qc.invalidateQueries({ queryKey: ["spaces"] });
      void navigate({
        to: "/drive/$spaceId",
        params: { spaceId: space.id },
        search: { dir: undefined },
      });
    },
  });

  return (
    <main className="bg-background flex min-h-svh items-center justify-center p-6">
      <Empty className="max-w-sm">
        <EmptyHeader>
          <EmptyMedia>
            {accept.isSuccess ? (
              <Check className="text-primary size-10" />
            ) : (
              <Mail className="text-muted-foreground size-10" />
            )}
          </EmptyMedia>
          <EmptyTitle>
            {accept.isError ? (
              <Trans>This invitation is not valid</Trans>
            ) : (
              <Trans>You have been invited</Trans>
            )}
          </EmptyTitle>
          <EmptyDescription>
            {accept.isError ? (
              (accept.error as Error).message
            ) : (
              <Trans>
                Accepting adds your account to the space. The invitation is tied to the address it
                was sent to.
              </Trans>
            )}
          </EmptyDescription>
        </EmptyHeader>
        {!accept.isError && (
          <Button disabled={accept.isPending} onClick={() => accept.mutate()}>
            {accept.isPending ? <Trans>Joining…</Trans> : <Trans>Accept invitation</Trans>}
          </Button>
        )}
        {accept.isError && (
          <Button variant="outline" onClick={() => void navigate({ to: "/drive" })}>
            <Trans>Go to Drive</Trans>
          </Button>
        )}
      </Empty>
    </main>
  );
}
