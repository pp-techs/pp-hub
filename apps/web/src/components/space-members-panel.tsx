import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Download, Mail, MoreHorizontal, Trash2, UserPlus } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Badge } from "@workspace/ui/components/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
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
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { fmtDate, fmtRelative, type MemberRole } from "@/lib/api";
import type { Invitation, SpaceMember, UserSearchResult } from "@/lib/api/types";
import {
  useAddSpaceMember,
  useCreateInvitation,
  useInvitations,
  useMyProfile,
  useRemoveSpaceMember,
  useRevokeInvitation,
  useSpaceMembers,
  useUpdateMemberRole,
} from "@/lib/queries";
import { MemberPicker } from "@/components/member-picker";
import { RailLabel } from "@/components/rail-item";

interface SpaceMembersPanelProps {
  spaceId: string;
  currentUserRole: string;
}

const ROLES: { value: MemberRole; label: string; blurb: string }[] = [
  {
    value: "admin",
    label: "Admin",
    blurb: "Manage members, groups and settings, delete the space, and open every folder in it",
  },
  {
    value: "member",
    label: "Member",
    blurb:
      "Opens only what their groups have been given access to — a role on its own grants nothing",
  },
];

const ROLE_ITEMS = ROLES.map((r) => ({ label: r.label, value: r.value }));

const ROLE_FILTERS = [
  { label: "Role: All", value: "all" },
  ...ROLES.map((r) => ({ label: `Role: ${r.label}`, value: r.value })),
];

const STATUS_FILTERS = [
  { label: "Status: All", value: "all" },
  { label: "Status: Active", value: "active" },
  { label: "Status: Pending", value: "pending" },
];

/**
 * Who belongs to a space, and on what terms.
 *
 * Members and pending invitations share one table rather than sitting in two
 * lists: from the owner's side they are the same question — who has access —
 * and splitting them hides the fact that an invitation is access already
 * granted, just not yet taken up. Pending rows are visually distinct and carry
 * their own actions (resend, revoke).
 *
 * The server already returns `files_owned`, `created_at` and `last_seen` per
 * member, so those columns cost nothing extra to render.
 */
export function SpaceMembersPanel({ spaceId, currentUserRole }: SpaceMembersPanelProps) {
  const { data: members = [], isLoading } = useSpaceMembers(spaceId);
  const isAdmin = currentUserRole === "admin";
  // Listing invitations is admin-gated on the server, so asking as a member
  // would only produce a 403 in the console.
  const invitations = useInvitations(spaceId, isAdmin);
  const { data: me } = useMyProfile();

  const updateRole = useUpdateMemberRole();
  const removeMember = useRemoveSpaceMember();
  const invite = useCreateInvitation(spaceId);
  const revokeInvite = useRevokeInvitation(spaceId);

  const [inviteOpen, setInviteOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");
  const [statusFilter, setStatusFilter] = useState("all");

  const pending = invitations.data ?? [];

  const visibleMembers = useMemo(() => {
    const q = query.trim().toLowerCase();
    return members.filter((m) => {
      if (statusFilter === "pending") return false;
      if (roleFilter !== "all" && m.role !== roleFilter) return false;
      if (!q) return true;
      return (
        m.email.toLowerCase().includes(q) ||
        (m.display_name || m.name || "").toLowerCase().includes(q)
      );
    });
  }, [members, query, roleFilter, statusFilter]);

  const visiblePending = useMemo(() => {
    const q = query.trim().toLowerCase();
    return pending.filter((i) => {
      if (statusFilter === "active") return false;
      if (roleFilter !== "all" && i.role !== roleFilter) return false;
      return !q || i.email.toLowerCase().includes(q);
    });
  }, [pending, query, roleFilter, statusFilter]);

  const shown = visibleMembers.length + visiblePending.length;
  const total = members.length + pending.length;

  if (isLoading) {
    return <p className="text-muted-foreground text-xs">Loading members…</p>;
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-sm font-medium">Members</h2>
          <p className="text-muted-foreground text-[10px] tabular-nums">
            {plural(members.length, "member")}
            {pending.length > 0 && ` · ${pending.length} pending`}
          </p>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {isAdmin && (
            <Button onClick={() => setInviteOpen(true)}>
              <UserPlus data-icon="inline-start" /> Invite people
            </Button>
          )}
          <Button
            variant="outline"
            disabled={total === 0}
            onClick={() => exportCsv(visibleMembers, visiblePending)}
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
          aria-label="Filter members by name or email"
          className="max-w-64"
        />
        <FilterSelect
          value={roleFilter}
          onChange={setRoleFilter}
          items={ROLE_FILTERS}
          label="Filter by role"
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
            <TableHead className="w-28">Role</TableHead>
            <TableHead className="w-28">Joined</TableHead>
            <TableHead className="w-28">Last active</TableHead>
            <TableHead className="w-24 text-right">Files owned</TableHead>
            {isAdmin && <TableHead className="w-32" />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {visibleMembers.map((member) => (
            <MemberRow
              key={member.user_id}
              member={member}
              isSelf={member.user_id === me?.id}
              external={isExternal(member.email, me?.email)}
              canManage={isAdmin}
              onRoleChange={(role) =>
                updateRole.mutate(
                  { spaceId, userId: member.user_id, role },
                  { onError: (e) => toast.error(e.message) },
                )
              }
              onRemove={() =>
                removeMember.mutate(
                  { spaceId, userId: member.user_id },
                  {
                    onSuccess: () => toast.success(`Removed ${member.email}`),
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
              busy={invite.isPending || revokeInvite.isPending}
              onResend={() =>
                invite.mutate(
                  { email: inv.email, role: inv.role },
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
              <TableCell colSpan={isAdmin ? 6 : 5} className="text-muted-foreground py-6 text-xs">
                No one matches these filters.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <div>
        <RailLabel>Role reference</RailLabel>
        <dl className="border-border divide-border grid gap-0 divide-y rounded-lg border sm:grid-cols-3 sm:divide-x sm:divide-y-0">
          {ROLES.map((r) => (
            <div key={r.value} className="flex flex-col gap-0.5 p-2">
              <dt className="text-xs font-medium">{r.label}</dt>
              <dd className="text-muted-foreground text-xs">{r.blurb}</dd>
            </div>
          ))}
        </dl>
      </div>

      {isAdmin && (
        <InviteDialog
          spaceId={spaceId}
          open={inviteOpen}
          onOpenChange={setInviteOpen}
          memberIds={members.map((m) => m.user_id)}
        />
      )}
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
  isSelf: boolean;
  external: boolean;
  canManage: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}

function MemberRow({
  member,
  isSelf,
  external,
  canManage,
  onRoleChange,
  onRemove,
}: MemberRowProps) {
  const name = member.display_name || member.name || member.email;

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
              {isSelf && <Chip>You</Chip>}
              {external && <ExternalChip />}
            </span>
            <span className="text-muted-foreground truncate text-[10px]">{member.email}</span>
          </div>
        </div>
      </TableCell>

      <TableCell>
        {/* A space must keep an owner, and the server refuses an owner
            demoting themselves — so that row is plain text, not a control
            that only fails. */}
        {canManage && !isSelf ? (
          <Select
            value={member.role}
            items={ROLE_ITEMS}
            onValueChange={(v) => v && onRoleChange(v as MemberRole)}
          >
            <SelectTrigger size="sm" aria-label={`Role for ${name}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ROLE_ITEMS.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <span className="text-muted-foreground capitalize">{member.role}</span>
        )}
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
          <div className="flex items-center justify-end">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${name}`}
                    disabled={isSelf}
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem variant="destructive" onClick={onRemove}>
                  <Trash2 data-icon="inline-start" /> Remove from space
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
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
      <TableCell className="text-muted-foreground capitalize">{invitation.role}</TableCell>
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

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <Badge variant="secondary" className="shrink-0">
      {children}
    </Badge>
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

/**
 * Adds someone to the space — by picking an existing account, or by emailing
 * an address that has none.
 *
 * Both paths end at the same place, so they share one dialog and one role
 * control rather than being two disconnected forms in the settings page.
 */
function InviteDialog({
  spaceId,
  open,
  onOpenChange,
  memberIds,
}: {
  spaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memberIds: string[];
}) {
  const addMember = useAddSpaceMember();
  const invite = useCreateInvitation(spaceId);

  const [role, setRole] = useState<MemberRole>("member");
  const [picked, setPicked] = useState<UserSearchResult | null>(null);
  const [email, setEmail] = useState("");

  const close = () => {
    onOpenChange(false);
    setPicked(null);
    setEmail("");
    setRole("member");
  };

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? onOpenChange(true) : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Invite people</DialogTitle>
          <DialogDescription>
            New members can see everything in this space. Choose what they may do with it.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <RailLabel>Role</RailLabel>
            <ToggleGroup value={[role]} onValueChange={(v) => v[0] && setRole(v[0] as MemberRole)}>
              {ROLES.map((r) => (
                <ToggleGroupItem key={r.value} value={r.value}>
                  {r.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>

          <div className="flex flex-col gap-1">
            <RailLabel>Someone with an account</RailLabel>
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1">
                <MemberPicker
                  onSelect={setPicked}
                  excludeIds={memberIds}
                  placeholder={picked ? picked.display_name || picked.name : "Search people…"}
                />
              </div>
              <Button
                disabled={!picked || addMember.isPending}
                onClick={() =>
                  picked &&
                  addMember.mutate(
                    { spaceId, user_id: picked.id, role },
                    {
                      onSuccess: () => {
                        toast.success(`Added ${picked.display_name || picked.name}`);
                        close();
                      },
                      onError: (e) => toast.error(e.message),
                    },
                  )
                }
              >
                Add
              </Button>
            </div>
          </div>

          <form
            className="flex flex-col gap-1"
            onSubmit={(e) => {
              e.preventDefault();
              invite.mutate(
                { email: email.trim(), role },
                {
                  onSuccess: (result) => {
                    if (result.added_member) {
                      toast.success(`${result.email} already has an account`, {
                        description: "They were added to the space directly.",
                      });
                    } else if (result.url) {
                      void navigator.clipboard?.writeText(result.url);
                      toast.success("Invitation created", {
                        description: "The link is on your clipboard — send it to them.",
                      });
                    }
                    close();
                  },
                  onError: (e) => toast.error("Could not invite", { description: e.message }),
                },
              );
            }}
          >
            <RailLabel>By email</RailLabel>
            <div className="flex items-center gap-2">
              <Input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="name@example.com"
                aria-label="Email to invite"
                className="min-w-0 flex-1"
                required
              />
              <Button type="submit" disabled={invite.isPending || !email.trim()}>
                <UserPlus data-icon="inline-start" /> Invite
              </Button>
            </div>
            <p className="text-muted-foreground text-[10px]">
              pp-hub does not send mail yet. The invitation link is copied to your clipboard so you
              can send it yourself; it expires in 14 days.
            </p>
          </form>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Downloads the rows currently on screen, filters included. */
function exportCsv(members: SpaceMember[], pending: Invitation[]) {
  const header = ["Name", "Email", "Role", "Status", "Joined", "Last active", "Files owned"];
  const rows = [
    ...members.map((m) => [
      m.display_name || m.name || "",
      m.email,
      m.role,
      "active",
      m.created_at,
      m.last_seen ?? "",
      String(m.files_owned),
    ]),
    ...pending.map((i) => [" ", i.email, i.role, "pending", i.created_at, "", "0"]),
  ];

  const csv = [header, ...rows].map((row) => row.map(csvCell).join(",")).join("\r\n");
  const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = "members.csv";
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
