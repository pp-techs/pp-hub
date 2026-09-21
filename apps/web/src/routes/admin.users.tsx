import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type * as React from "react";
import { ArrowLeft } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Badge } from "@workspace/ui/components/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table";
import { cn } from "@workspace/ui/lib/utils";
import {
  adminStatsOptions,
  requireAuth,
  useAdminStats,
  useMe,
  useUpdateAdminUser,
  useUsers,
  usersOptions,
} from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { pageTitle } from "@/lib/i18n";
import { UserAvatar } from "@/components/user-avatar";
import { SectionLabel } from "@/components/section-label";

export const Route = createFileRoute("/admin/users")({
  head: () => ({ meta: [{ title: pageTitle(msg`Server administration`) }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  // `requireAuth` above has already primed the identity cache under keys.me.
  loader: ({ context }) => {
    void context.queryClient.ensureQueryData(adminStatsOptions());
    void context.queryClient.ensureQueryData(usersOptions(100, 0));
  },
  component: AdminPage,
});

/**
 * Server administration: the whole install, not one space.
 *
 * Distinct from space settings — a space owner administers their space, an
 * admin administers the server, and neither role implies the other. The server
 * enforces the same distinction, so this page rendering is not what keeps a
 * non-admin out.
 */
function AdminPage() {
  const { data: me } = useMe();
  const stats = useAdminStats();
  const { data, isLoading } = useUsers(100, 0);
  const update = useUpdateAdminUser();
  const [query, setQuery] = useState("");
  const { t } = useLingui();
  const f = useFormatters();

  const users = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = data?.users ?? [];
    return q
      ? all.filter(
          (u) =>
            u.email.toLowerCase().includes(q) ||
            (u.display_name || u.name).toLowerCase().includes(q),
        )
      : all;
  }, [data, query]);

  if (me && !me.is_admin) {
    return (
      <main className="flex min-h-svh flex-col items-center justify-center gap-3 p-6">
        <p className="text-muted-foreground text-sm">
          <Trans>Admin access required.</Trans>
        </p>
        <Button variant="outline" render={<Link to="/drive" />}>
          <Trans>Back to Drive</Trans>
        </Button>
      </main>
    );
  }

  // Named locals so the ICU messages read `{trashed, plural, …}` rather than
  // `{0, plural, …}`: the placeholder name is the only hint a translator has.
  const trashed = stats.data?.trashed ?? 0;
  const failed = stats.data?.jobs_failed ?? 0;

  return (
    <main className="bg-background text-foreground flex min-h-svh flex-col">
      <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t`Back to Drive`}
          render={<Link to="/drive" />}
        >
          <ArrowLeft />
        </Button>
        <div className="flex flex-col">
          <span className="text-sm font-medium">
            <Trans>Server administration</Trans>
          </span>
          <span className="text-muted-foreground text-[10px]">
            <Trans>Every user and space on this install</Trans>
          </span>
        </div>
      </header>

      <div className="flex flex-col gap-6 p-4">
        <section>
          <SectionLabel>
            <Trans>Overview</Trans>
          </SectionLabel>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
            <StatTile
              label={<Trans>Users</Trans>}
              value={stats.data?.users}
              note={suspendedNote(stats.data?.suspended_users)}
            />
            <StatTile label={<Trans>Spaces</Trans>} value={stats.data?.spaces} />
            <StatTile
              label={<Trans>Files</Trans>}
              value={stats.data?.files}
              note={<Plural value={trashed} one="# in trash" other="# in trash" />}
            />
            <StatTile
              label={<Trans>Storage used</Trans>}
              value={stats.data ? f.size(stats.data.storage_bytes) : undefined}
              note={<Trans>referenced blobs</Trans>}
            />
            <StatTile
              label={<Trans>Preview queue</Trans>}
              value={stats.data?.jobs_pending}
              note={
                failed ? (
                  <Plural value={failed} one="# failed" other="# failed" />
                ) : (
                  <Trans>none failed</Trans>
                )
              }
              alert={failed > 0}
            />
          </div>
        </section>

        <section className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <SectionLabel>
              <Trans>Users</Trans>
            </SectionLabel>
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t`Filter by name or email`}
              aria-label={t`Filter users`}
              className="max-w-64"
            />
          </div>

          {isLoading ? (
            <p className="text-muted-foreground text-xs">
              <Trans>Loading…</Trans>
            </p>
          ) : (
            <Table className="text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead>
                    <Trans>User</Trans>
                  </TableHead>
                  <TableHead className="w-20">
                    <Trans>Spaces</Trans>
                  </TableHead>
                  <TableHead className="w-32">
                    <Trans>Last seen</Trans>
                  </TableHead>
                  <TableHead className="w-24">
                    <Trans>Status</Trans>
                  </TableHead>
                  <TableHead className="w-48" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u) => {
                  const isSelf = u.id === me?.id;
                  const email = u.email;
                  return (
                    <TableRow key={u.id} className={cn(u.suspended && "bg-destructive/5")}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <UserAvatar
                            src={u.avatar_url}
                            name={u.display_name || u.name}
                            email={u.email}
                            size="sm"
                          />
                          <div className="flex min-w-0 flex-col">
                            <span className="flex items-center gap-1 truncate font-medium">
                              {u.display_name || u.name}
                              {isSelf && (
                                <Badge>
                                  <Trans>You</Trans>
                                </Badge>
                              )}
                            </span>
                            <span className="text-muted-foreground truncate text-[10px]">
                              {u.email}
                            </span>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground tabular-nums">
                        {u.spaces ?? "—"}
                      </TableCell>
                      <TableCell className="text-muted-foreground tabular-nums">
                        {u.last_seen ? f.relative(u.last_seen) : <Trans>never</Trans>}
                      </TableCell>
                      <TableCell>
                        <span
                          className={cn(
                            "rounded px-1 py-0.5 text-[10px] font-medium tracking-wider uppercase",
                            u.suspended
                              ? "bg-destructive/10 text-destructive"
                              : "text-muted-foreground",
                          )}
                        >
                          {u.suspended ? <Trans>Suspended</Trans> : <Trans>Active</Trans>}
                        </span>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center justify-end gap-1">
                          {/* Both guards mirror the server, which refuses an
                              admin's attempt to lock themselves out. */}
                          <Button
                            variant="ghost"
                            size="xs"
                            disabled={isSelf && u.is_admin}
                            onClick={() =>
                              update.mutate(
                                { userId: u.id, is_admin: !u.is_admin },
                                { onError: (e) => toast.error(e.message) },
                              )
                            }
                          >
                            {u.is_admin ? <Trans>Revoke admin</Trans> : <Trans>Make admin</Trans>}
                          </Button>
                          <Button
                            variant={u.suspended ? "ghost" : "destructive"}
                            size="xs"
                            disabled={isSelf}
                            onClick={() =>
                              update.mutate(
                                { userId: u.id, suspended: !u.suspended },
                                {
                                  onSuccess: () =>
                                    toast.success(
                                      u.suspended ? t`${email} reinstated` : t`${email} suspended`,
                                    ),
                                  onError: (e) => toast.error(e.message),
                                },
                              )
                            }
                          >
                            {u.suspended ? <Trans>Reinstate</Trans> : <Trans>Suspend</Trans>}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </section>
      </div>
    </main>
  );
}

/**
 * Rendered rather than returned as a string: `# suspended` is a plural, and
 * a plural only survives translation as ICU inside the tree.
 */
function suspendedNote(n?: number): React.ReactNode {
  if (n === undefined) return undefined;
  return n > 0 ? (
    <Plural value={n} one="# suspended" other="# suspended" />
  ) : (
    <Trans>all active</Trans>
  );
}

interface StatTileProps {
  label: React.ReactNode;
  value?: number | string;
  note?: React.ReactNode;
  alert?: boolean;
}

function StatTile({ label, value, note, alert }: StatTileProps) {
  return (
    <div className="border-border flex flex-col gap-0.5 rounded-lg border p-3">
      <span className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
        {label}
      </span>
      <span className="text-xl font-semibold tabular-nums">{value ?? "—"}</span>
      {note && (
        <span className={cn("text-[10px]", alert ? "text-destructive" : "text-muted-foreground")}>
          {note}
        </span>
      )}
    </div>
  );
}
