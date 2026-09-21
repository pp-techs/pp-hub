import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { Monitor, Smartphone, Tablet } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Session } from "@/lib/api";
import { useRevokeOtherSessions, useRevokeSession, useSessions } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { describeAgent } from "@/lib/user-agent";
import { SectionLabel } from "@/components/section-label";

/** Form factor -> icon. These are icon components, not labels: nothing to translate. */
const FORM_ICONS = { desktop: Monitor, phone: Smartphone, tablet: Tablet } as const;

const RevokeOthersDialog = createModal<{ count: number }, boolean>(
  "revoke-other-sessions",
  ({ count, modal }) => (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            <Trans>Sign out everywhere else?</Trans>
          </DialogTitle>
          <DialogDescription>
            <Plural
              value={count}
              one="The other signed-in device will have to sign in again."
              other="All # other signed-in devices will have to sign in again."
            />{" "}
            <Trans>This device stays signed in.</Trans>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => modal.resolve(false)}>
            <Trans>Cancel</Trans>
          </Button>
          <Button variant="destructive" onClick={() => modal.resolve(true)}>
            <Trans>Sign out everywhere else</Trans>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ),
);

/**
 * Every device currently holding a live session, and the means to end one.
 *
 * The point of the pane is the row you do not recognise: sessions are rows in
 * the database now, so an old laptop can be signed out from here rather than
 * by rotating a secret and knocking everybody out at once.
 */
export function AccountSecurityPane() {
  const { t } = useLingui();
  const { data: sessions, isLoading, isError } = useSessions();
  const revoke = useRevokeSession();
  const revokeOthers = useRevokeOtherSessions();

  const others = sessions?.filter((s) => !s.current) ?? [];

  async function signOutEverywhereElse() {
    const confirmed = await RevokeOthersDialog.show({ count: others.length });
    if (!confirmed) return;
    revokeOthers.mutate(undefined, {
      onSuccess: () => toast.success(t`Signed out everywhere else`),
      onError: (e) => toast.error(t`Could not sign out other devices`, { description: e.message }),
    });
  }

  return (
    <section className="flex max-w-2xl flex-col gap-3">
      <SectionLabel>
        <Trans>Signed-in devices</Trans>
      </SectionLabel>
      <div className="flex items-start justify-between gap-4">
        <p className="text-muted-foreground text-[10px]">
          <Trans>
            Each row is a live session. Signing one out takes effect immediately — the device is
            asked to sign in again the next time it reaches the server.
          </Trans>
        </p>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          disabled={others.length === 0 || revokeOthers.isPending}
          onClick={() => void signOutEverywhereElse()}
        >
          <Trans>Sign out everywhere else</Trans>
        </Button>
      </div>

      {isLoading && (
        <p className="text-muted-foreground text-xs">
          <Trans>Loading…</Trans>
        </p>
      )}
      {isError && (
        <p className="text-destructive text-xs">
          <Trans>Could not load your sessions.</Trans>
        </p>
      )}

      {sessions && (
        <ul className="border-border divide-border divide-y rounded-md border">
          {sessions.map((session) => (
            <SessionRow
              key={session.id}
              session={session}
              busy={revoke.isPending && revoke.variables === session.id}
              onRevoke={() =>
                revoke.mutate(session.id, {
                  onSuccess: () => toast.success(t`Device signed out`),
                  onError: (e) =>
                    toast.error(t`Could not sign out that device`, { description: e.message }),
                })
              }
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function SessionRow({
  session,
  busy,
  onRevoke,
}: {
  session: Session;
  busy: boolean;
  onRevoke: () => void;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  // Browser and platform names are proper nouns, so `label` is shown exactly
  // as `lib/user-agent.ts` produced it; only the line around it is translated.
  const { label, form } = describeAgent(session.user_agent);
  const Icon = FORM_ICONS[form];
  const ip = session.ip || t`Unknown IP`;
  const lastActive = f.relative(session.last_used_at);
  const signedIn = f.date(session.created_at);

  return (
    <li className="flex items-center gap-3 px-3 py-2.5">
      <Icon className="text-muted-foreground size-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="truncate text-xs font-medium">{label}</span>
          {session.current && (
            <Badge variant="secondary" className="shrink-0">
              <Trans>This device</Trans>
            </Badge>
          )}
        </div>
        <span className="text-muted-foreground truncate text-[10px]">
          <Trans>
            {ip} · last active {lastActive} · signed in {signedIn}
          </Trans>
        </span>
      </div>
      {!session.current && (
        <Button variant="ghost" size="sm" className="shrink-0" disabled={busy} onClick={onRevoke}>
          <Trans>Revoke</Trans>
        </Button>
      )}
    </li>
  );
}
