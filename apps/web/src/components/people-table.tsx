import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Download, Lock, Mail, Plus, Trash2, UserPlus, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Badge } from "@workspace/ui/components/badge";
import { Switch } from "@workspace/ui/components/switch";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
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
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { fmtDate, fmtRelative } from "@/lib/api";
import type { Group, Invitation, Space, SpaceMember } from "@/lib/api";
import {
  useAddGroupMember,
  useCreateInvitation,
  useGroupMemberships,
  useGroups,
  useInvitations,
  useMyProfile,
  useRemoveGroupMember,
  useRemoveSpaceMember,
  useRevokeInvitation,
  useSetMemberAdmin,
  useSpaceMembers,
} from "@/lib/queries";
import { InviteModal } from "@/components/invite-modal";

const STATUS_FILTERS = [
  { label: "Status: All", value: "all" },
  { label: "Status: Active", value: "active" },
  { label: "Status: Pending", value: "pending" },
];

const ALL_GROUPS = "all";

interface PeopleTableProps {
  space: Space;
  /** Which group the drawer is showing — the table filters to match. */
  groupFilter: string | undefined;
  onGroupFilterChange: (groupId: string | undefined) => void;
}

/**
 * Everyone in a space, in one table: who they are, whether they administer the
 * space, and which groups they belong to.
 *
 * One table rather than a Members tab plus a Groups tab. Admin rights and group
 * membership are both answers to "what is this person to this space", and
 * splitting them produced a UI where promoting someone happened in one place
 * while what they could actually open was decided in another. Pending
 * invitations are rows here too: an invitation is access already granted, just
 * not yet taken up.
 *
 * What a group *opens* is still set on the folder, from its access dialog. This
 * table says who is in the group; the folder says what the group reaches.
 */
export function PeopleTable({ space, groupFilter, onGroupFilterChange }: PeopleTableProps) {
  const spaceId = space.id;
  const isAdmin = space.is_admin;

  const { data: members = [], isLoading } = useSpaceMembers(spaceId);
  // Listing invitations is admin-gated on the server, so asking as a member
  // would only produce a 403 in the console.
  const invitations = useInvitations(spaceId, isAdmin);
  const { data: groups = [] } = useGroups(spaceId);
  const { byUser } = useGroupMemberships(spaceId, groups);
  const { data: me } = useMyProfile();

  const setAdmin = useSetMemberAdmin();
  const removeMember = useRemoveSpaceMember();
  const invite = useCreateInvitation(spaceId);
  const revokeInvite = useRevokeInvitation(spaceId);
  const addToGroup = useAddGroupMember(spaceId);
  const removeFromGroup = useRemoveGroupMember(spaceId);

  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");

  const pending = invitations.data ?? [];

  const groupItems = useMemo(
    () => [
      { label: "Group: All", value: ALL_GROUPS },
      ...groups.map((g) => ({ label: `Group: ${g.name}`, value: g.id })),
    ],
    [groups],
  );

  const visibleMembers = useMemo(() => {
    const q = query.trim().toLowerCase();
    return members.filter((m) => {
      if (statusFilter === "pending") return false;
      if (groupFilter && !(byUser.get(m.user_id) ?? []).some((g) => g.id === groupFilter)) {
        return false;
      }
      if (!q) return true;
      return (
        m.email.toLowerCase().includes(q) ||
        (m.display_name || m.name || "").toLowerCase().includes(q)
      );
    });
  }, [members, query, statusFilter, groupFilter, byUser]);

  const visiblePending = useMemo(() => {
    const q = query.trim().toLowerCase();
    // An invitation is not in any group yet, so a group filter excludes them
    // all rather than showing rows the filter does not describe.
    if (groupFilter) return [];
    return pending.filter((i) => {
      if (statusFilter === "active") return false;
      return !q || i.email.toLowerCase().includes(q);
    });
  }, [pending, query, statusFilter, groupFilter]);

  const shown = visibleMembers.length + visiblePending.length;
  const total = members.length + pending.length;

  if (isLoading) {
    return <p className="text-muted-foreground text-xs">Loading people…</p>;
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-sm font-medium">People</h2>
          <p className="text-muted-foreground text-[10px] tabular-nums">
            {plural(members.length, "member")}
            {pending.length > 0 && ` · ${pending.length} pending`} ·{" "}
            {plural(groups.length, "group")}
          </p>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {isAdmin && (
            <Button
              onClick={() =>
                void InviteModal.show({
                  spaceId,
                  memberIds: members.map((m) => m.user_id),
                  defaultAsAdmin: space.default_as_admin ?? false,
                })
              }
            >
              <UserPlus data-icon="inline-start" /> Invite people
            </Button>
          )}
          <Button
            variant="outline"
            disabled={total === 0}
            onClick={() => exportCsv(visibleMembers, visiblePending, byUser)}
          >
            <Download data-icon="inline-start" /> Export CSV
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter by name or email"
          aria-label="Filter people by name or email"
          className="max-w-64"
        />
        <FilterSelect
          value={groupFilter ?? ALL_GROUPS}
          onChange={(v) => onGroupFilterChange(v === ALL_GROUPS ? undefined : v)}
          items={groupItems}
          label="Filter by group"
        />
        <FilterSelect
          value={statusFilter}
          onChange={setStatusFilter}
          items={STATUS_FILTERS}
          label="Filter by status"
        />
        <span className="text-muted-foreground ml-auto shrink-0 text-[10px] tabular-nums">
          Showing {shown} of {total}
        </span>
      </div>

      <Table className="text-xs">
        <TableHeader>
          <TableRow>
            <TableHead>Member</TableHead>
            <TableHead className="w-16">Admin</TableHead>
            <TableHead className="w-64">Groups</TableHead>
            <TableHead className="w-24">Joined</TableHead>
            <TableHead className="w-24">Last active</TableHead>
            <TableHead className="w-20 text-right">Files owned</TableHead>
            {isAdmin && <TableHead className="w-10" />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {visibleMembers.map((member) => (
            <MemberRow
              key={member.user_id}
              member={member}
              groups={groups}
              held={byUser.get(member.user_id) ?? []}
              isSelf={member.user_id === me?.id}
              external={isExternal(member.email, me?.email)}
              canManage={isAdmin}
              onAdminToggle={(as_admin) =>
                setAdmin.mutate(
                  { spaceId, userId: member.user_id, as_admin },
                  { onError: (e) => toast.error(e.message) },
                )
              }
              onAddToGroup={(group) =>
                addToGroup.mutate(
                  { groupId: group.id, userId: member.user_id },
                  {
                    onSuccess: () => toast.success(`Added to ${group.name}`),
                    onError: (e) => toast.error(e.message),
                  },
                )
              }
              onRemoveFromGroup={(group) =>
                removeFromGroup.mutate(
                  { groupId: group.id, userId: member.user_id },
                  {
                    onSuccess: () => toast.success(`Removed from ${group.name}`),
                    onError: (e) => toast.error(e.message),
                  },
                )
              }
              onOpenGroup={(group) => onGroupFilterChange(group.id)}
              onRemove={() =>
                removeMember.mutate(
                  { spaceId, userId: member.user_id },
                  {
                    onSuccess: () =>
                      toast.success(`Removed ${member.email}`, {
                        description: "Anything they owned here is filed in the Archive.",
                      }),
                    onError: (e) => toast.error(e.message),
                  },
                )
              }
            />
          ))}

          {visiblePending.map((inv) => (
            <PendingRow
              key={inv.id}
              invitation={inv}
              external={isExternal(inv.email, me?.email)}
              canManage={isAdmin}
              busy={invite.isPending}
              onResend={() =>
                invite.mutate(
                  { email: inv.email, as_admin: inv.as_admin },
                  {
                    onSuccess: (result) => {
                      if (result.url) {
                        void navigator.clipboard?.writeText(result.url);
                        toast.success("New invitation link copied", {
                          description: `Send it to ${inv.email} — the previous link no longer works.`,
                        });
                      }
                    },
                    onError: (e) => toast.error(e.message),
                  },
                )
              }
              onRevoke={() =>
                revokeInvite.mutate(inv.id, {
                  onSuccess: () => toast.success(`Revoked the invitation for ${inv.email}`),
                  onError: (e) => toast.error(e.message),
                })
              }
            />
          ))}

          {shown === 0 && (
            <TableRow>
              <TableCell colSpan={isAdmin ? 7 : 6} className="text-muted-foreground py-6 text-xs">
                No one matches these filters.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}

function FilterSelect({
  value,
  onChange,
  items,
  label,
}: {
  value: string;
  onChange: (v: string) => void;
  items: { label: string; value: string }[];
  label: string;
}) {
  return (
    <Select value={value} items={items} onValueChange={(v) => onChange((v as string) ?? value)}>
      <SelectTrigger aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

interface MemberRowProps {
  member: SpaceMember;
  /** Every group in the space, for the add menu. */
  groups: Group[];
  /** The groups this member is in. */
  held: Group[];
  isSelf: boolean;
  external: boolean;
  canManage: boolean;
  onAdminToggle: (as_admin: boolean) => void;
  onAddToGroup: (group: Group) => void;
  onRemoveFromGroup: (group: Group) => void;
  onOpenGroup: (group: Group) => void;
  onRemove: () => void;
}

function MemberRow({
  member,
  groups,
  held,
  isSelf,
  external,
  canManage,
  onAdminToggle,
  onAddToGroup,
  onRemoveFromGroup,
  onOpenGroup,
  onRemove,
}: MemberRowProps) {
  const name = member.display_name || member.name || member.email;
  const joinable = groups.filter(
    (g) => !g.is_everyone && !g.is_admin && !held.some((h) => h.id === g.id),
  );

  return (
    <TableRow>
      <TableCell>
        <div className="flex min-w-0 items-center gap-2">
          <Avatar size="sm" className="shrink-0">
            <AvatarImage src={member.avatar_url} />
            <AvatarFallback>{name.slice(0, 2).toUpperCase()}</AvatarFallback>
          </Avatar>
          <div className="flex min-w-0 flex-col leading-tight">
            <span className="flex items-center gap-1 truncate font-medium">
              <span className="truncate">{name}</span>
              {isSelf && <Badge variant="secondary">You</Badge>}
              {external && <ExternalChip />}
            </span>
            <span className="text-muted-foreground truncate text-[10px]">{member.email}</span>
          </div>
        </div>
      </TableCell>

      <TableCell>
        {/* Admin is membership of the automatic Admin group, so this switch and
            that group are the same fact — which is why the group itself is
            read-only in the drawer. */}
        <Switch
          size="sm"
          checked={member.is_admin}
          disabled={!canManage || isSelf}
          aria-label={`Admin of this space: ${name}`}
          title={
            isSelf
              ? "You cannot change your own admin rights"
              : member.is_admin
                ? "Remove admin rights"
                : "Make admin"
          }
          onCheckedChange={onAdminToggle}
        />
      </TableCell>

      <TableCell>
        <div className="flex flex-wrap items-center gap-1">
          {held.map((g) => {
            const automatic = g.is_everyone || g.is_admin;
            return (
              <span
                key={g.id}
                className="border-border text-muted-foreground inline-flex items-center gap-1 rounded-sm border px-1.5 text-[10px]"
              >
                {automatic && <Lock className="size-2.5 shrink-0" />}
                <button
                  type="button"
                  className="hover:text-foreground max-w-24 truncate"
                  onClick={() => onOpenGroup(g)}
                >
                  {g.name}
                </button>
                {canManage && !automatic && (
                  <button
                    type="button"
                    aria-label={`Remove ${member.email} from ${g.name}`}
                    className="hover:text-destructive shrink-0"
                    onClick={() => onRemoveFromGroup(g)}
                  >
                    <X className="size-2.5" />
                  </button>
                )}
              </span>
            );
          })}

          {canManage && joinable.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon-xs" aria-label={`Add ${name} to a group`} />
                }
              >
                <Plus />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                {joinable.map((g) => (
                  <DropdownMenuItem key={g.id} onClick={() => onAddToGroup(g)}>
                    {g.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </TableCell>

      <TableCell className="text-muted-foreground tabular-nums">
        {fmtDate(member.created_at)}
      </TableCell>
      <TableCell className="text-muted-foreground tabular-nums">
        {member.last_seen ? fmtRelative(member.last_seen) : "never"}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {member.files_owned.toLocaleString()}
      </TableCell>

      {canManage && (
        <TableCell>
          <Button
            variant="ghost"
            size="icon-xs"
            disabled={isSelf}
            onClick={onRemove}
            aria-label={`Remove ${member.email} from the space`}
            title={isSelf ? "You cannot remove yourself" : "Remove from space"}
          >
            <Trash2 />
          </Button>
        </TableCell>
      )}
    </TableRow>
  );
}

interface PendingRowProps {
  invitation: Invitation;
  external: boolean;
  canManage: boolean;
  busy: boolean;
  onResend: () => void;
  onRevoke: () => void;
}

function PendingRow({
  invitation,
  external,
  canManage,
  busy,
  onResend,
  onRevoke,
}: PendingRowProps) {
  return (
    <TableRow className="bg-muted/20">
      <TableCell>
        <div className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden
            className="bg-muted text-muted-foreground grid size-6 shrink-0 place-content-center rounded-full"
          >
            <Mail className="size-3" />
          </span>
          <span className="flex min-w-0 items-center gap-1">
            <span className="text-muted-foreground truncate italic">{invitation.email}</span>
            <Badge variant="outline" className="text-primary border-primary/30 bg-primary/10">
              Pending
            </Badge>
            {external && <ExternalChip />}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <Switch
          size="sm"
          checked={invitation.as_admin}
          disabled
          aria-label="Will join as admin"
          title={invitation.as_admin ? "Will join as an admin" : "Will join as a member"}
        />
      </TableCell>
      <TableCell className="text-muted-foreground">—</TableCell>
      <TableCell className="text-muted-foreground tabular-nums">
        Invited {fmtDate(invitation.created_at)}
      </TableCell>
      <TableCell className="text-muted-foreground">—</TableCell>
      <TableCell className="text-muted-foreground text-right">—</TableCell>
      {canManage && (
        <TableCell>
          <div className="flex items-center justify-end gap-1">
            <Button variant="ghost" size="xs" disabled={busy} onClick={onResend}>
              Resend
            </Button>
            <Button variant="destructive" size="xs" disabled={busy} onClick={onRevoke}>
              Revoke
            </Button>
          </div>
        </TableCell>
      )}
    </TableRow>
  );
}

/** Flags an address outside the signed-in user's own mail domain. */
function ExternalChip() {
  return (
    <Badge
      variant="outline"
      className="shrink-0"
      title="This address is on a different mail domain from yours"
    >
      External
    </Badge>
  );
}

function isExternal(email: string, myEmail?: string): boolean {
  const mine = myEmail?.split("@")[1]?.toLowerCase();
  const theirs = email.split("@")[1]?.toLowerCase();
  return !!mine && !!theirs && mine !== theirs;
}

/** Downloads the rows currently on screen, filters included. */
function exportCsv(members: SpaceMember[], pending: Invitation[], byUser: Map<string, Group[]>) {
  const header = [
    "Name",
    "Email",
    "Admin",
    "Groups",
    "Status",
    "Joined",
    "Last active",
    "Files owned",
  ];
  const rows = [
    ...members.map((m) => [
      m.display_name || m.name || "",
      m.email,
      m.is_admin ? "Yes" : "No",
      (byUser.get(m.user_id) ?? []).map((g) => g.name).join(" | "),
      "active",
      m.created_at,
      m.last_seen ?? "",
      String(m.files_owned),
    ]),
    ...pending.map((i) => [
      " ",
      i.email,
      i.as_admin ? "Yes" : "No",
      "",
      "pending",
      i.created_at,
      "",
      "0",
    ]),
  ];

  const csv = [header, ...rows].map((row) => row.map(csvCell).join(",")).join("\r\n");
  const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = "people.csv";
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Quotes a CSV field. The leading apostrophe on =, +, - and @ stops a
 * spreadsheet treating a member's own display name as a formula.
 */
function csvCell(value: string): string {
  const safe = /^[=+\-@]/.test(value) ? `'${value}` : value;
  return `"${safe.replace(/"/g, '""')}"`;
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}
