import { createFileRoute } from "@tanstack/react-router";
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
import { fmtDate, fmtRelative, type Session } from "@/lib/api";
import { useRevokeOtherSessions, useRevokeSession, useSessions } from "@/lib/queries";
import { Surface } from "@/components/surface";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/drive/account/sessions")({
  head: () => ({ meta: [{ title: "Sessions · Account · pp-hub" }] }),
  component: SessionsPage,
});

/** Edge and Opera also say "Chrome", and Chrome also says "Safari" — order is the test. */
const BROWSERS: [match: RegExp, name: string][] = [
  [/\bEdgi?A?\//, "Edge"],
  [/\bOPR\//, "Opera"],
  [/\bFirefox\//, "Firefox"],
  [/\bChrome\//, "Chrome"],
  [/\bSafari\//, "Safari"],
];

const PLATFORMS: [match: RegExp, name: string, form: FormFactor][] = [
  [/\biPhone\b/, "iPhone", "phone"],
  [/\biPad\b/, "iPad", "tablet"],
  [/\bAndroid\b/, "Android", "phone"],
  [/\bCrOS\b/, "ChromeOS", "desktop"],
  [/\bMac OS X\b|\bMacintosh\b/, "macOS", "desktop"],
  [/\bWindows\b/, "Windows", "desktop"],
  [/\bLinux\b/, "Linux", "desktop"],
];

type FormFactor = "desktop" | "phone" | "tablet";

const FORM_ICONS = { desktop: Monitor, phone: Smartphone, tablet: Tablet } as const;

/**
 * "Chrome on macOS" out of a user-agent string.
 *
 * Deliberately crude: this line only has to let somebody recognise their own
 * laptop in a list of three, and a full UA database would be a dependency
 * carried for one sentence. Anything unrecognised — a script, a mobile app,
 * a new browser — shows its raw agent rather than a wrong guess.
 */
function describeAgent(ua: string): { label: string; form: FormFactor } {
  const browser = BROWSERS.find(([match]) => match.test(ua))?.[1];
  const platform = PLATFORMS.find(([match]) => match.test(ua));

  if (browser && platform) return { label: `${browser} on ${platform[1]}`, form: platform[2] };
  if (platform) return { label: platform[1], form: platform[2] };
  if (browser) return { label: browser, form: "desktop" };
  return { label: ua.trim() || "Unknown device", form: "desktop" };
}

const RevokeOthersDialog = createModal<{ count: number }, boolean>(
  "revoke-other-sessions",
  ({ count, modal }) => (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Sign out everywhere else?</DialogTitle>
          <DialogDescription>
            {count === 1
              ? "The other signed-in device will have to sign in again."
              : `All ${count} other signed-in devices will have to sign in again.`}{" "}
            This device stays signed in.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => modal.resolve(false)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={() => modal.resolve(true)}>
            Sign out everywhere else
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  ),
);

/**
 * Every device currently holding a live session, and the means to end one.
 *
 * The point of the page is the row you do not recognise: sessions are rows in
 * the database now, so an old laptop can be signed out from here rather than
 * by rotating a secret and knocking everybody out at once.
 */
function SessionsPage() {
  const { data: sessions, isLoading, isError } = useSessions();
  const revoke = useRevokeSession();
  const revokeOthers = useRevokeOtherSessions();

  const others = sessions?.filter((s) => !s.current) ?? [];

  async function signOutEverywhereElse() {
    const confirmed = await RevokeOthersDialog.show({ count: others.length });
    if (!confirmed) return;
    revokeOthers.mutate(undefined, {
      onSuccess: () => toast.success("Signed out everywhere else"),
      onError: (e) => toast.error("Could not sign out other devices", { description: e.message }),
    });
  }

  return (
    <Surface className="max-w-2xl">
      <section className="flex flex-col gap-3">
        <SectionLabel>Signed-in devices</SectionLabel>
        <div className="flex items-start justify-between gap-4">
          <p className="text-muted-foreground text-[10px]">
            Each row is a live session. Signing one out takes effect immediately — the device is
            asked to sign in again the next time it reaches the server.
          </p>
          <Button
            variant="outline"
            size="sm"
            className="shrink-0"
            disabled={others.length === 0 || revokeOthers.isPending}
            onClick={() => void signOutEverywhereElse()}
          >
            Sign out everywhere else
          </Button>
        </div>

        {isLoading && <p className="text-muted-foreground text-xs">Loading…</p>}
        {isError && <p className="text-destructive text-xs">Could not load your sessions.</p>}

        {sessions && (
          <ul className="border-border divide-border divide-y rounded-md border">
            {sessions.map((session) => (
              <SessionRow
                key={session.id}
                session={session}
                busy={revoke.isPending && revoke.variables === session.id}
                onRevoke={() =>
                  revoke.mutate(session.id, {
                    onSuccess: () => toast.success("Device signed out"),
                    onError: (e) =>
                      toast.error("Could not sign out that device", { description: e.message }),
                  })
                }
              />
            ))}
          </ul>
        )}
      </section>
    </Surface>
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
  const { label, form } = describeAgent(session.user_agent);
  const Icon = FORM_ICONS[form];

  return (
    <li className="flex items-center gap-3 px-3 py-2.5">
      <Icon className="text-muted-foreground size-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="truncate text-xs font-medium">{label}</span>
          {session.current && (
            <Badge variant="secondary" className="shrink-0">
              This device
            </Badge>
          )}
        </div>
        <span className="text-muted-foreground truncate text-[10px]">
          {session.ip || "Unknown IP"} · last active {fmtRelative(session.last_used_at)} · signed in{" "}
          {fmtDate(session.created_at)}
        </span>
      </div>
      {!session.current && (
        <Button variant="ghost" size="sm" className="shrink-0" disabled={busy} onClick={onRevoke}>
          Revoke
        </Button>
      )}
    </li>
  );
}
