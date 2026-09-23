import { createFileRoute, redirect } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { I18n, MessageDescriptor } from "@lingui/core";
import { Download, X } from "lucide-react";

import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { AdminHeader } from "@/components/admin-header";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { cn } from "@workspace/ui/lib/utils";
import { type SecurityEvent, type SecurityEventFilters } from "@/lib/api";
import { securityEventsExportUrl } from "@/lib/api/users";
import { requireAuth, SECURITY_PAGE_SIZE, useSecurityEvents } from "@/lib/queries";
import { describeAgent } from "@/lib/user-agent";
import { useFormatters } from "@/lib/format";
import { i18n as globalI18n, pageTitle } from "@/lib/i18n";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/admin/security")({
  head: () => ({ meta: [{ title: pageTitle(msg`Security log`) }] }),
  // Gate here, not in the component. A page that resolves and then says
  // "admin only" has already told the caller that this URL exists and what it
  // is for; the redirect says nothing.
  beforeLoad: async ({ context, location }) => {
    const me = await requireAuth(context.queryClient, location.href);
    if (!me.is_admin) {
      // `beforeLoad` runs outside React, so the singleton answers here the
      // same way it does for `pageTitle` above.
      toast.error(globalI18n._(msg`Only an administrator can read the security log.`));
      throw redirect({ to: "/drive" });
    }
  },
  component: SecurityLogPage,
});

/** "All" is a sentinel, not a kind: the server filters on an exact string, so
 *  the parameter is omitted rather than sent empty. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- filter sentinel, not a server kind
const ALL = "all";

/**
 * The `kind` values the server writes, in the order this page offers them,
 * and what each one is called on screen. Descriptors, not strings: this is
 * module scope, evaluated at import time before a catalog is active, so a
 * rendered label here would freeze the first language loaded.
 *
 * Spelling every kind out rather than de-underscoring the identifier is what
 * makes them translatable at all — `member_added` is a column value, not a
 * phrase any other language can derive. The keys are the list of kinds: a
 * second array beside this one could only drift out of step with it.
 */
const KIND_LABELS = {
  login: msg`login`,
  login_failed: msg`login failed`,
  logout: msg`logout`,
  session_revoked: msg`session revoked`,
  sessions_revoked: msg`sessions revoked`,
  identity_adopted: msg`identity adopted`,
  admin_granted: msg`admin granted`,
  admin_revoked: msg`admin revoked`,
  account_suspended: msg`account suspended`,
  account_unsuspended: msg`account unsuspended`,
  grant_created: msg`grant created`,
  grant_revoked: msg`grant revoked`,
  share_created: msg`share created`,
  share_revoked: msg`share revoked`,
  member_added: msg`member added`,
  member_removed: msg`member removed`,
  group_created: msg`group created`,
  group_updated: msg`group updated`,
  group_deleted: msg`group deleted`,
  group_member_added: msg`group member added`,
  group_member_removed: msg`group member removed`,
  file_downloaded: msg`file downloaded`,
  folder_downloaded: msg`folder downloaded`,
  file_viewed: msg`file viewed`,
  file_opened: msg`opened in editor`,
  security_alert: msg`security alert`,
  security_log_exported: msg`security log exported`,
} satisfies Record<string, MessageDescriptor>;

/** What a `security_alert` row's subject — the rule that fired — reads as. */
const RULE_LABELS: Record<string, MessageDescriptor> = {
  login_failed_burst: msg`Repeated failed sign-ins`,
};

/** The subject the server writes for a read made through a share link. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- server subject value
const LINK_SUBJECT = "share_link";

const ALL_EVENTS = msg`All events`;

/**
 * The local midnight `days` after a `YYYY-MM-DD` date input value, as the
 * instant the server filters on. Built from parts rather than parsed, because
 * a bare date string parses as UTC midnight, not local.
 */
function localMidnight(day: string, days = 0): string {
  const [y = 0, m = 1, d = 1] = day.split("-").map(Number);
  return new Date(y, m - 1, d + days).toISOString();
}

/**
 * The date inputs' local days as the range the server filters on: `from` is
 * that day's midnight, `to` the midnight after the chosen day, because the
 * server's `to` is exclusive.
 */
function dayRange(fromDate: string, toDate: string): Pick<SecurityEventFilters, "from" | "to"> {
  return {
    from: fromDate ? localMidnight(fromDate) : undefined,
    to: toDate ? localMidnight(toDate, 1) : undefined,
  };
}

/**
 * A kind the server wrote but this build does not know about still has to
 * render as something readable, so the identifier is the fallback.
 */
function kindLabel(i18n: I18n, kind: string): string {
  const descriptor = KIND_LABELS[kind as keyof typeof KIND_LABELS];
  return descriptor ? i18n._(descriptor) : kind.replace(/_/g, " ");
}

/**
 * The instance security log: who signed in, who failed to, every change to
 * who can reach what, and who read which file.
 *
 * The page exists for one shape in particular. A single `login_failed` is
 * noise — somebody mistyped a password. A *run* of them, from one IP, across
 * several accounts and inside a few minutes, is an attack in progress, and
 * nothing else in the product would show it: those rows carry no `user_id`
 * and no `user_email` at all, so they appear on no user's own session list.
 * Seeing them requires exactly this view — instance-wide, newest first,
 * filterable by kind, with the address on every row — which is why
 * `login_failed`, and the `security_alert` a run of them raises, are the
 * kinds rendered in the destructive variant.
 *
 * The other question it answers is "who read this": a row's file name
 * narrows the log to that file, or everything under that folder.
 */
function SecurityLogPage() {
  const { t, i18n } = useLingui();
  const [kind, setKind] = useState<string>(ALL);
  const [node, setNode] = useState<{ id: string; name: string } | null>(null);
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [offset, setOffset] = useState(0);
  const filter = kind === ALL ? undefined : kind;
  const filters: SecurityEventFilters = {
    kind: filter,
    node: node?.id,
    ...dayRange(fromDate, toDate),
  };
  const { data, isLoading } = useSecurityEvents(filters, offset);
  // A filter is a different result set; keeping the offset would land page 4
  // of a list that now has one page.
  const filterNode = (id: string, name: string) => {
    setNode({ id, name });
    setOffset(0);
  };
  const nodeName = node?.name;

  const events = data?.events ?? [];
  const total = data?.total ?? 0;
  const from = total === 0 ? 0 : offset + 1;
  const to = offset + events.length;
  // Rebuilt every render rather than memoised: it has to follow the active
  // catalog, and a few dozen lookups is cheaper than getting that wrong.
  const kindItems = [
    { label: i18n._(ALL_EVENTS), value: ALL },
    ...Object.entries(KIND_LABELS).map(([k, label]) => ({ label: i18n._(label), value: k })),
  ];
  const filterLabel = filter ? kindLabel(i18n, filter) : null;

  return (
    <main className="bg-background text-foreground flex min-h-svh flex-col">
      <AdminHeader
        title={<Trans>Security log</Trans>}
        subtitle={
          <Trans>Sign-ins, failures, access changes and file access across this install</Trans>
        }
        // eslint-disable-next-line lingui/no-unlocalized-strings -- admin tab identifier
        activeTab="security"
      />

      <div className="flex flex-col gap-6 p-4">
        <section className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <SectionLabel>
              <Trans>Events</Trans>
            </SectionLabel>
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={kind}
                items={kindItems}
                onValueChange={(v) => {
                  setKind((v as string) ?? ALL);
                  setOffset(0);
                }}
              >
                <SelectTrigger aria-label={t`Filter by event kind`} className="w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {kindItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                type="date"
                aria-label={t`From date`}
                className="w-36"
                value={fromDate}
                max={toDate || undefined}
                onChange={(e) => {
                  setFromDate(e.target.value);
                  setOffset(0);
                }}
              />
              <Input
                type="date"
                aria-label={t`To date`}
                className="w-36"
                value={toDate}
                min={fromDate || undefined}
                onChange={(e) => {
                  setToDate(e.target.value);
                  setOffset(0);
                }}
              />
              {/* Follows the filters on screen: what is exported is what
                  the reader is looking at, every page of it. */}
              <Button
                variant="outline"
                size="sm"
                render={<a href={securityEventsExportUrl(filters)} />}
              >
                <Download data-icon="inline-start" /> <Trans>Export CSV</Trans>
              </Button>
            </div>
          </div>

          {node && (
            <div>
              <Badge variant="outline" className="gap-1 pr-0.5">
                <Trans>File: {nodeName}</Trans>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t`Clear file filter`}
                  onClick={() => {
                    setNode(null);
                    setOffset(0);
                  }}
                >
                  <X />
                </Button>
              </Badge>
            </div>
          )}

          {isLoading ? (
            <p className="text-muted-foreground text-xs">
              <Trans>Loading…</Trans>
            </p>
          ) : events.length === 0 ? (
            <p className="text-muted-foreground text-xs">
              {node || fromDate || toDate ? (
                <Trans>No events match these filters.</Trans>
              ) : filterLabel ? (
                <Trans>No events recorded for {filterLabel}.</Trans>
              ) : (
                <Trans>No events recorded.</Trans>
              )}
            </p>
          ) : (
            <Table className="text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-32">
                    <Trans>When</Trans>
                  </TableHead>
                  <TableHead className="w-40">
                    <Trans>Event</Trans>
                  </TableHead>
                  <TableHead>
                    <Trans>Who</Trans>
                  </TableHead>
                  <TableHead className="w-36">
                    <Trans>IP</Trans>
                  </TableHead>
                  <TableHead className="w-44">
                    <Trans>Device</Trans>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((e) => (
                  <EventRow key={e.id} event={e} onFilterNode={filterNode} />
                ))}
              </TableBody>
            </Table>
          )}

          <div className="flex items-center justify-between gap-2">
            <span className="text-muted-foreground text-[10px] tabular-nums">
              <Trans>
                {from}–{to} of {total}
              </Trans>
            </span>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="xs"
                disabled={offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - SECURITY_PAGE_SIZE))}
              >
                <Trans>Previous</Trans>
              </Button>
              <Button
                variant="outline"
                size="xs"
                disabled={to >= total}
                onClick={() => setOffset((o) => o + SECURITY_PAGE_SIZE)}
              >
                <Trans>Next</Trans>
              </Button>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}

function EventRow({
  event,
  onFilterNode,
}: {
  event: SecurityEvent;
  onFilterNode: (id: string, name: string) => void;
}) {
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const failed = event.kind === "login_failed" || event.kind === "security_alert";
  const rule = event.kind === "security_alert" ? RULE_LABELS[event.subject ?? ""] : undefined;
  // A failed sign-in has no account behind it, so `subject` — the address
  // tried, or the reason it was refused — is the only identity there is. A
  // link visitor has no account either, and its subject is a marker, not a
  // name.
  const who = rule
    ? i18n._(rule)
    : event.subject === LINK_SUBJECT && !event.user_id
      ? t`Share link visitor`
      : event.user_name || event.user_email || event.subject || "—";
  const detail = describeDetail(event.detail);

  return (
    <TableRow className={cn(failed && "bg-destructive/5")}>
      <TableCell className="text-muted-foreground tabular-nums" title={f.date(event.created_at)}>
        {f.relative(event.created_at)}
      </TableCell>
      <TableCell>
        <Badge variant={failed ? "destructive" : "secondary"}>{kindLabel(i18n, event.kind)}</Badge>
      </TableCell>
      {/* max-w-0 lets this column take the remaining width and truncate,
          rather than a long detail line widening the whole table; the
          minimum keeps it readable on a narrow screen. */}
      <TableCell className="max-w-0 min-w-48">
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium">{who}</span>
          {event.user_email && event.user_name && (
            <span className="text-muted-foreground truncate text-[10px]">{event.user_email}</span>
          )}
          {event.node_id && (
            <Button
              variant="link"
              size="xs"
              className="h-auto max-w-full justify-start px-0"
              title={t`Show all events for this item`}
              onClick={() => onFilterNode(event.node_id!, event.node_name || event.node_id!)}
            >
              <span className="truncate">{event.node_name || event.node_id}</span>
            </Button>
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
