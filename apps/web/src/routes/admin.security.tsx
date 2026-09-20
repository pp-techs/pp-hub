import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { ArrowLeft } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { cn } from "@workspace/ui/lib/utils";
import { fmtDate, fmtRelative, type SecurityEvent } from "@/lib/api";
import { requireAuth, SECURITY_PAGE_SIZE, useSecurityEvents } from "@/lib/queries";
import { describeAgent } from "@/lib/user-agent";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/admin/security")({
  head: () => ({ meta: [{ title: "Security log · pp-hub" }] }),
  // Gate here, not in the component. A page that resolves and then says
  // "admin only" has already told the caller that this URL exists and what it
  // is for; the redirect says nothing.
  beforeLoad: async ({ context, location }) => {
    const me = await requireAuth(context.queryClient, location.href);
    if (!me.is_admin) {
      toast.error("Only an administrator can read the security log.");
      throw redirect({ to: "/drive" });
    }
  },
  component: SecurityLogPage,
});

/** The `kind` values the server writes, in the order this page offers them. */
const KINDS = [
  "login",
  "login_failed",
  "logout",
  "session_revoked",
  "sessions_revoked",
  "identity_adopted",
  "admin_granted",
  "admin_revoked",
  "account_suspended",
  "account_unsuspended",
  "grant_created",
  "grant_revoked",
  "share_created",
  "share_revoked",
  "member_added",
  "member_removed",
] as const;

/** "All" is a sentinel, not a kind: the server filters on an exact string, so
 *  the parameter is omitted rather than sent empty. */
const ALL = "all";

const KIND_ITEMS = [
  { label: "All events", value: ALL },
  ...KINDS.map((kind) => ({ label: kind.replace(/_/g, " "), value: kind })),
];

/**
 * The instance security log: who signed in, who failed to, and every change
 * to who can reach what.
 *
 * The page exists for one shape in particular. A single `login_failed` is
 * noise — somebody mistyped a password. A *run* of them, from one IP, across
 * several accounts and inside a few minutes, is an attack in progress, and
 * nothing else in the product would show it: those rows carry no `user_id`
 * and no `user_email` at all, so they appear on no user's own session list.
 * Seeing them requires exactly this view — instance-wide, newest first,
 * filterable by kind, with the address on every row — which is why
 * `login_failed` is the one kind rendered in the destructive variant.
 */
function SecurityLogPage() {
  const [kind, setKind] = useState<string>(ALL);
  const [offset, setOffset] = useState(0);
  const filter = kind === ALL ? undefined : kind;
  const { data, isLoading } = useSecurityEvents(filter, offset);

  const events = data?.events ?? [];
  const total = data?.total ?? 0;
  const from = total === 0 ? 0 : offset + 1;
  const to = offset + events.length;

  return (
    <main className="bg-background text-foreground flex min-h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Back to Drive"
          render={<Link to="/drive" />}
        >
          <ArrowLeft />
        </Button>
        <div className="flex flex-col">
          <span className="text-sm font-medium">Security log</span>
          <span className="text-muted-foreground text-[10px]">
            Sign-ins, failures and access changes across this install
          </span>
        </div>
        <Button variant="ghost" size="sm" className="ml-auto" render={<Link to="/admin/users" />}>
          Users
        </Button>
      </header>

      <div className="flex flex-col gap-6 p-4">
        <section className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <SectionLabel>Events</SectionLabel>
            <div className="flex items-center gap-2">
              <Select
                value={kind}
                items={KIND_ITEMS}
                onValueChange={(v) => {
                  setKind((v as string) ?? ALL);
                  // A filter is a different result set; keeping the offset
                  // would land page 4 of a list that now has one page.
                  setOffset(0);
                }}
              >
                <SelectTrigger aria-label="Filter by event kind" className="w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KIND_ITEMS.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {isLoading ? (
            <p className="text-muted-foreground text-xs">Loading…</p>
          ) : events.length === 0 ? (
            <p className="text-muted-foreground text-xs">
              No events recorded{filter ? ` for ${filter.replace(/_/g, " ")}` : ""}.
            </p>
          ) : (
            <Table className="text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-32">When</TableHead>
                  <TableHead className="w-40">Event</TableHead>
                  <TableHead>Who</TableHead>
                  <TableHead className="w-36">IP</TableHead>
                  <TableHead className="w-44">Device</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((e) => (
                  <EventRow key={e.id} event={e} />
                ))}
              </TableBody>
            </Table>
          )}

          <div className="flex items-center justify-between gap-2">
            <span className="text-muted-foreground text-[10px] tabular-nums">
              {from}–{to} of {total}
            </span>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="xs"
                disabled={offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - SECURITY_PAGE_SIZE))}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="xs"
                disabled={to >= total}
                onClick={() => setOffset((o) => o + SECURITY_PAGE_SIZE)}
              >
                Next
              </Button>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}

function EventRow({ event }: { event: SecurityEvent }) {
  const failed = event.kind === "login_failed";
  // A failed sign-in has no account behind it, so `subject` — the address
  // tried, or the reason it was refused — is the only identity there is.
  const who = event.user_name || event.user_email || event.subject || "—";
  const detail = describeDetail(event.detail);

  return (
    <TableRow className={cn(failed && "bg-destructive/5")}>
      <TableCell className="text-muted-foreground tabular-nums" title={fmtDate(event.created_at)}>
        {fmtRelative(event.created_at)}
      </TableCell>
      <TableCell>
        <Badge variant={failed ? "destructive" : "secondary"}>
          {event.kind.replace(/_/g, " ")}
        </Badge>
      </TableCell>
      <TableCell>
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium">{who}</span>
          {event.user_email && event.user_name && (
            <span className="text-muted-foreground truncate text-[10px]">{event.user_email}</span>
          )}
          {detail && <span className="text-muted-foreground truncate text-[10px]">{detail}</span>}
        </div>
      </TableCell>
      <TableCell className="text-muted-foreground tabular-nums">{event.ip || "—"}</TableCell>
      <TableCell className="text-muted-foreground truncate" title={event.user_agent}>
        {event.user_agent ? describeAgent(event.user_agent).label : "—"}
      </TableCell>
    </TableRow>
  );
}

/**
 * `detail` is whatever the handler that wrote the row chose to attach, so it
 * is `unknown` and every shape has to be survived rather than assumed: a
 * string, an object, a number, or JSON that will not stringify (a cycle
 * cannot arrive over the wire, but a `BigInt` reviver would). It is a hint
 * beside the row, never the row itself — so anything unreadable renders as
 * nothing at all rather than as "[object Object]".
 */
function describeDetail(detail: unknown): string | null {
  if (detail == null) return null;
  if (typeof detail === "string") return detail || null;
  if (typeof detail === "number" || typeof detail === "boolean") return String(detail);
  if (typeof detail !== "object") return null;
  try {
    const text = Object.entries(detail as Record<string, unknown>)
      .filter(([, v]) => v != null && typeof v !== "object")
      .map(([k, v]) => `${k}: ${String(v)}`)
      .join(" · ");
    return text || JSON.stringify(detail).slice(0, 160) || null;
  } catch {
    return null;
  }
}
