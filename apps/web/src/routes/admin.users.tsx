import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import {
  Activity,
  Boxes,
  Files,
  HardDrive,
  MoreHorizontal,
  Search,
  ShieldCheck,
  ShieldOff,
  UserCheck,
  UserX,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type * as React from "react";

import { AdminPage } from "@/components/admin-page";
import { Button } from "@workspace/ui/components/button";
import { Badge } from "@workspace/ui/components/badge";
import { Card } from "@workspace/ui/components/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@workspace/ui/components/input-group";
import { Skeleton } from "@workspace/ui/components/skeleton";
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
  useAdminStats,
  useMe,
  useUpdateAdminUser,
  useUsers,
  usersOptions,
} from "@/lib/queries";
import type { AdminUser } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { useLocale } from "@/components/locale-provider";
import { pageTitle } from "@/lib/i18n";
import { UserAvatar } from "@/components/user-avatar";
import { SkeletonTableRows } from "@/components/loading";

export const Route = createFileRoute("/admin/users")({
  head: () => ({ meta: [{ title: pageTitle(msg`Server administration`) }] }),
  // The `/admin` layout has already authenticated the caller and primed the
  // identity cache under keys.me.
  loader: ({ context }) => {
    void context.queryClient.ensureQueryData(adminStatsOptions());
    void context.queryClient.ensureQueryData(usersOptions(100, 0));
  },
  component: AdminUsersPage,
});

/**
 * Server administration: the whole install, not one space.
 *
 * Distinct from space settings — a space owner administers their space, an
 * admin administers the server, and neither role implies the other. The server
 * enforces the same distinction, so this page rendering is not what keeps a
 * non-admin out.
 */
function AdminUsersPage() {
  const { data: me } = useMe();
  const stats = useAdminStats();
  const { data, isLoading } = useUsers(100, 0);
  const [query, setQuery] = useState("");
  const { t } = useLingui();
  const f = useFormatters();
  const locale = useLocale();

  const all = data?.users;
  const users = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = all ?? [];
    return q
      ? list.filter(
          (u) =>
            u.email.toLowerCase().includes(q) ||
            (u.display_name || u.name).toLowerCase().includes(q),
        )
      : list;
  }, [all, query]);

  // Named locals so the ICU messages read `{trashed, plural, …}` rather than
  // `{0, plural, …}`: the placeholder name is the only hint a translator has.
  const trashed = stats.data?.trashed ?? 0;
  const failed = stats.data?.jobs_failed ?? 0;
  const shown = users.length;
  const total = data?.total ?? shown;

  return (
    <AdminPage
      title={<Trans>Server administration</Trans>}
      description={<Trans>Every user and space on this install</Trans>}
    >
      <section
        aria-label={t`Overview`}
        className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5"
      >
        <StatTile
          icon={Users}
          label={<Trans>Users</Trans>}
          value={stats.data?.users}
          note={suspendedNote(stats.data?.suspended_users)}
        />
        <StatTile icon={Boxes} label={<Trans>Spaces</Trans>} value={stats.data?.spaces} />
        <StatTile
          icon={Files}
          label={<Trans>Files</Trans>}
          value={stats.data ? new Intl.NumberFormat(locale).format(stats.data.files) : undefined}
          note={<Plural value={trashed} one="# in trash" other="# in trash" />}
        />
        <StatTile
          icon={HardDrive}
          label={<Trans>Storage used</Trans>}
          value={stats.data ? f.size(stats.data.storage_bytes) : undefined}
          note={<Trans>referenced blobs</Trans>}
        />
        <StatTile
          icon={Activity}
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
      </section>

      <Card className="gap-0 py-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b p-3">
          <InputGroup className="w-full sm:max-w-72">
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t`Filter by name or email`}
              aria-label={t`Filter users`}
            />
          </InputGroup>
          <span className="text-muted-foreground text-xs tabular-nums">
            {isLoading ? (
              <Skeleton className="h-3.5 w-24" />
            ) : (
              <Trans>
                {shown} of {total} users
              </Trans>
            )}
          </span>
        </div>

        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="h-9 pl-4 text-[11px] tracking-wider uppercase">
                <Trans>User</Trans>
              </TableHead>
              <TableHead className="h-9 w-28 text-[11px] tracking-wider uppercase">
                <Trans>Role</Trans>
              </TableHead>
              <TableHead className="h-9 w-20 text-[11px] tracking-wider uppercase">
                <Trans>Spaces</Trans>
              </TableHead>
              <TableHead className="h-9 w-36 text-[11px] tracking-wider uppercase">
                <Trans>Last seen</Trans>
              </TableHead>
              <TableHead className="h-9 w-28 text-[11px] tracking-wider uppercase">
                <Trans>Status</Trans>
              </TableHead>
              <TableHead className="h-9 w-14 pr-4" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <SkeletonTableRows columns={6} />
            ) : users.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="text-muted-foreground py-10 text-center text-sm">
                  <Trans>No users match this filter.</Trans>
                </TableCell>
              </TableRow>
            ) : (
              users.map((u) => (
                <UserRow key={u.id} user={u} isSelf={u.id === me?.id} lastSeen={f.relative} />
              ))
            )}
          </TableBody>
        </Table>
      </Card>
    </AdminPage>
  );
}

function UserRow({
  user: u,
  isSelf,
  lastSeen,
}: {
  user: AdminUser;
  isSelf: boolean;
  lastSeen: (iso: string) => string;
}) {
  const { t } = useLingui();
  const update = useUpdateAdminUser();
  const email = u.email;
  const name = u.display_name || u.name;

  return (
    <TableRow className={cn("h-14", u.suspended && "bg-destructive/5")}>
      <TableCell className="pl-4">
        <div className="flex items-center gap-3">
          <UserAvatar src={u.avatar_url} name={name} email={u.email} size="sm" />
          <div className="flex min-w-0 flex-col">
            <span className="flex items-center gap-1.5 truncate text-sm font-medium">
              {name}
              {isSelf && (
                <Badge variant="outline">
                  <Trans>You</Trans>
                </Badge>
              )}
            </span>
            <span className="text-muted-foreground truncate text-xs">{u.email}</span>
          </div>
        </div>
      </TableCell>
      <TableCell>
        {u.is_admin ? (
          <Badge variant="secondary">
            <ShieldCheck />
            <Trans>Admin</Trans>
          </Badge>
        ) : (
          <span className="text-muted-foreground text-xs">
            <Trans>Member</Trans>
          </span>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground text-sm tabular-nums">
        {u.spaces ?? "—"}
      </TableCell>
      <TableCell className="text-muted-foreground text-sm">
        {u.last_seen ? lastSeen(u.last_seen) : <Trans>never</Trans>}
      </TableCell>
      <TableCell>
        <span className="inline-flex items-center gap-1.5 text-xs">
          <span
            aria-hidden
            className={cn(
              "size-1.5 rounded-full",
              u.suspended ? "bg-destructive" : "bg-emerald-500",
            )}
          />
          <span className={cn(u.suspended ? "text-destructive font-medium" : "text-foreground")}>
            {u.suspended ? <Trans>Suspended</Trans> : <Trans>Active</Trans>}
          </span>
        </span>
      </TableCell>
      <TableCell className="pr-4 text-right">
        {/* Both guards mirror the server, which refuses an admin's attempt to
            lock themselves out. A menu with nothing enabled is not offered. */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" aria-label={t`Actions for ${email}`}>
                <MoreHorizontal />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem
              disabled={isSelf && u.is_admin}
              onClick={() =>
                update.mutate(
                  { userId: u.id, is_admin: !u.is_admin },
                  { onError: (e) => toast.error(e.message) },
                )
              }
            >
              {u.is_admin ? (
                <>
                  <ShieldOff /> <Trans>Revoke admin</Trans>
                </>
              ) : (
                <>
                  <ShieldCheck /> <Trans>Make admin</Trans>
                </>
              )}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant={u.suspended ? "default" : "destructive"}
              disabled={isSelf}
              onClick={() =>
                update.mutate(
                  { userId: u.id, suspended: !u.suspended },
                  {
                    onSuccess: () =>
                      toast.success(u.suspended ? t`${email} reinstated` : t`${email} suspended`),
                    onError: (e) => toast.error(e.message),
                  },
                )
              }
            >
              {u.suspended ? (
                <>
                  <UserCheck /> <Trans>Reinstate</Trans>
                </>
              ) : (
                <>
                  <UserX /> <Trans>Suspend</Trans>
                </>
              )}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
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
  icon: LucideIcon;
  label: React.ReactNode;
  value?: number | string;
  note?: React.ReactNode;
  alert?: boolean;
}

function StatTile({ icon: Icon, label, value, note, alert }: StatTileProps) {
  return (
    <Card className="gap-2 py-3.5">
      <div className="flex items-center justify-between gap-2 px-4">
        <span className="text-muted-foreground text-xs font-medium">{label}</span>
        <span
          className={cn(
            "flex size-6 items-center justify-center rounded-md [&_svg]:size-3.5",
            alert ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground",
          )}
        >
          <Icon />
        </span>
      </div>
      <div className="flex flex-col gap-0.5 px-4">
        {value === undefined ? (
          <Skeleton className="h-7 w-16" />
        ) : (
          <span className="text-2xl leading-tight font-semibold tracking-tight tabular-nums">
            {value}
          </span>
        )}
        {note && (
          <span className={cn("text-xs", alert ? "text-destructive" : "text-muted-foreground")}>
            {note}
          </span>
        )}
      </div>
    </Card>
  );
}
