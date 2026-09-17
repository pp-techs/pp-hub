import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { toast } from "sonner";
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
import { fmtRelative, fmtSize } from "@/lib/api";
import {
  requireAuth,
  useAdminStats,
  useMyProfile,
  useUpdateAdminUser,
  useUsers,
} from "@/lib/queries";
import { UserAvatar } from "@/components/user-avatar";
import { RailLabel } from "@/components/rail-item";

export const Route = createFileRoute("/admin/users")({
  head: () => ({ meta: [{ title: "Server administration · pp-hub" }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
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
  const { data: me } = useMyProfile();
  const stats = useAdminStats();
  const { data, isLoading } = useUsers(100, 0);
  const update = useUpdateAdminUser();
  const [query, setQuery] = useState("");

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
        <p className="text-muted-foreground text-sm">Admin access required.</p>
        <Button variant="outline" render={<Link to="/drive" />}>
          Back to Drive
        </Button>
      </main>
    );
  }

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
          <span className="text-sm font-medium">Server administration</span>
          <span className="text-muted-foreground text-[10px]">
            Every user and space on this install
          </span>
        </div>
      </header>

      <div className="flex flex-col gap-6 p-4">
        <section>
          <RailLabel>Overview</RailLabel>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
            <StatTile
              label="Users"
              value={stats.data?.users}
              note={suspendedNote(stats.data?.suspended_users)}
            />
            <StatTile label="Spaces" value={stats.data?.spaces} />
            <StatTile
              label="Files"
              value={stats.data?.files}
              note={`${stats.data?.trashed ?? 0} in trash`}
            />
            <StatTile
              label="Storage used"
              value={stats.data ? fmtSize(stats.data.storage_bytes) : undefined}
              note="referenced blobs"
            />
            <StatTile
              label="Preview queue"
              value={stats.data?.jobs_pending}
              note={stats.data?.jobs_failed ? `${stats.data.jobs_failed} failed` : "none failed"}
              alert={!!stats.data?.jobs_failed}
            />
          </div>
        </section>

        <section className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <RailLabel>Users</RailLabel>
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by name or email"
              aria-label="Filter users"
              className="max-w-64"
            />
          </div>

          {isLoading ? (
            <p className="text-muted-foreground text-xs">Loading…</p>
          ) : (
            <Table className="text-xs">
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead className="w-20">Spaces</TableHead>
                  <TableHead className="w-32">Last seen</TableHead>
                  <TableHead className="w-24">Status</TableHead>
                  <TableHead className="w-48" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u) => {
                  const isSelf = u.id === me?.id;
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
                              {isSelf && <Badge>You</Badge>}
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
                        {u.last_seen ? fmtRelative(u.last_seen) : "never"}
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
                          {u.suspended ? "Suspended" : "Active"}
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
                            {u.is_admin ? "Revoke admin" : "Make admin"}
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
                                      u.suspended
                                        ? `${u.email} reinstated`
                                        : `${u.email} suspended`,
                                    ),
                                  onError: (e) => toast.error(e.message),
                                },
                              )
                            }
                          >
                            {u.suspended ? "Reinstate" : "Suspend"}
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

function suspendedNote(n?: number): string | undefined {
  if (n === undefined) return undefined;
  return n > 0 ? `${n} suspended` : "all active";
}

interface StatTileProps {
  label: string;
  value?: number | string;
  note?: string;
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
