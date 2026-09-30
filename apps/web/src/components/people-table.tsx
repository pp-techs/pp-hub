import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg, plural } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import {
  Download,
  Lock,
  MoreHorizontal,
  ShieldCheck,
  ShieldOff,
  UserMinus,
  UserPlus,
  UserRoundX,
  Users,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Badge } from "@workspace/ui/components/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
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
import { SkeletonTableRows } from "@/components/loading";
import type { DepartureSource, Group, Space, SpaceDeparture, SpaceMember } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import {
  useAddGroupMember,
  useCompleteDeparture,
  useGroupMemberships,
  useGroups,
  useMe,
  useRemoveGroupMember,
  useRemoveSpaceMember,
  useSetMemberAdmin,
  useSpaceDepartures,
  useSpaceMembers,
} from "@/lib/queries";
import { InviteModal } from "@/components/invite-modal";
import { UnlinkedSpaceAlert } from "@/components/unlinked-space-alert";
import { ConfirmDialog } from "@/components/confirm-dialog";

// eslint-disable-next-line lingui/no-unlocalized-strings -- sentinel for "no group filter", not copy
const ALL_GROUPS = "all";

/**
 * How pp-hub found out that somebody had gone. "An admin removed them here"
 * and "they vanished from the directory" read very differently to the person
 * about to confirm an irreversible handover, so the row says which it was.
 *
 * Descriptors rather than rendered strings: this is module scope, evaluated
 * once at import time and before any catalog is active, so text here would
 * pin the first language loaded and never follow a switch. The row resolves
 * them with `i18n._()` at render.
 */
const DEPARTURE_SOURCE: Record<DepartureSource, MessageDescriptor> = {
  signin: msg`Noticed when they last signed in`,
  webhook: msg`Reported by the identity provider`,
  reconcile: msg`Found by the hourly check`,
  admin: msg`Removed from this space here`,
};

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
 * while what they could actually open was decided in another.
 *
 * Below it sits the one thing membership no longer answers on its own: people
 * the organization has already dropped, whose access ended with the removal
 * but whose content is still theirs until an admin hands it over.
 *
 * What a group *opens* is still set on the folder, from its access dialog. This
 * table says who is in the group; the folder says what the group reaches.
 */
export function PeopleTable({ space, groupFilter, onGroupFilterChange }: PeopleTableProps) {
  const { t } = useLingui();
  const spaceId = space.id;
  const isAdmin = space.is_admin;

  const { data: members = [], isLoading } = useSpaceMembers(spaceId);
  // Listing pending handovers is admin-gated on the server, so asking as a
  // member would only produce a 403 in the console.
  const { data: departures = [] } = useSpaceDepartures(spaceId, isAdmin);
  const { data: groups = [] } = useGroups(spaceId);
  const { byUser } = useGroupMemberships(spaceId, groups);
  const { data: me } = useMe();

  const setAdmin = useSetMemberAdmin();
  const removeMember = useRemoveSpaceMember();
  const completeHandover = useCompleteDeparture(spaceId);
  const addToGroup = useAddGroupMember(spaceId);
  const removeFromGroup = useRemoveGroupMember(spaceId);

  const [query, setQuery] = useState("");
  const [handover, setHandover] = useState<SpaceDeparture | null>(null);
  const [removing, setRemoving] = useState<SpaceMember | null>(null);

  // Not memoised: the labels are translated per render so they follow a
  // language switch, and a three- or ten-item map costs nothing next to the
  // table itself.
  const groupItems = [
    { label: t`Group: All`, value: ALL_GROUPS },
    ...groups.map((g) => {
      const groupName = g.name;
      return { label: t`Group: ${groupName}`, value: g.id };
    }),
  ];

  const visibleMembers = useMemo(() => {
    const q = query.trim().toLowerCase();
    return members.filter((m) => {
      if (groupFilter && !(byUser.get(m.user_id) ?? []).some((g) => g.id === groupFilter)) {
        return false;
      }
      if (!q) return true;
      return (
        m.email.toLowerCase().includes(q) ||
        (m.display_name || m.name || "").toLowerCase().includes(q)
      );
    });
  }, [members, query, groupFilter, byUser]);

  const shown = visibleMembers.length;
  const total = members.length;

  const leaver = handover;
  const leaverName = leaver ? leaver.display_name || leaver.name || leaver.email : "";
  const owned = leaver?.files_owned ?? 0;
  const removingName = removing ? removing.display_name || removing.name || removing.email : "";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        {!space.idp_org_id && <UnlinkedSpaceAlert space={space} />}
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="flex flex-col gap-0.5">
            <h2 className="font-heading text-sm font-medium">
              <Trans>People</Trans>
            </h2>
            <p className="text-muted-foreground text-[10px] tabular-nums">
              <Plural value={total} one="# member" other="# members" />
              {" · "}
              <Plural value={groups.length} one="# group" other="# groups" />
            </p>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            {isAdmin && (
              <Button
                disabled={!space.idp_org_id}
                onClick={() =>
                  void InviteModal.show({
                    spaceId,
                    defaultAsAdmin: space.default_as_admin ?? false,
                  })
                }
              >
                <UserPlus data-icon="inline-start" /> <Trans>Invite people</Trans>
              </Button>
            )}
            <Button
              variant="outline"
              disabled={total === 0}
              onClick={() => exportCsv(visibleMembers, byUser)}
            >
              <Download data-icon="inline-start" /> <Trans>Export CSV</Trans>
            </Button>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t`Filter by name or email`}
            aria-label={t`Filter people by name or email`}
            className="max-w-64"
          />
          <FilterSelect
            value={groupFilter ?? ALL_GROUPS}
            onChange={(v) => onGroupFilterChange(v === ALL_GROUPS ? undefined : v)}
            items={groupItems}
            label={t`Filter by group`}
          />
          <span className="text-muted-foreground ml-auto shrink-0 text-[10px] tabular-nums">
            <Trans>
              Showing {shown} of {total}
            </Trans>
          </span>
        </div>

        <Table className="text-xs">
          <TableHeader>
            <TableRow>
              <TableHead>
                <Trans>Member</Trans>
              </TableHead>
              <TableHead className="w-64">
                <Trans>Groups</Trans>
              </TableHead>
              <TableHead className="w-24">
                <Trans>Joined</Trans>
              </TableHead>
              <TableHead className="w-24">
                <Trans>Last active</Trans>
              </TableHead>
              <TableHead className="w-20 text-right">
                <Trans>Files owned</Trans>
              </TableHead>
              {isAdmin && <TableHead className="w-10" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && <SkeletonTableRows columns={isAdmin ? 6 : 5} />}
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
                onAddToGroup={(group) => {
                  const groupName = group.name;
                  addToGroup.mutate(
                    { groupId: group.id, userId: member.user_id },
                    {
                      onSuccess: () => toast.success(t`Added to ${groupName}`),
                      onError: (e) => toast.error(e.message),
                    },
                  );
                }}
                onRemoveFromGroup={(group) => {
                  const groupName = group.name;
                  removeFromGroup.mutate(
                    { groupId: group.id, userId: member.user_id },
                    {
                      onSuccess: () => toast.success(t`Removed from ${groupName}`),
                      onError: (e) => toast.error(e.message),
                    },
                  );
                }}
                onOpenGroup={(group) => onGroupFilterChange(group.id)}
                onRemove={() => setRemoving(member)}
              />
            ))}

            {!isLoading && shown === 0 && (
              <TableRow>
                <TableCell colSpan={isAdmin ? 6 : 5} className="text-muted-foreground py-6 text-xs">
                  <Trans>No one matches these filters.</Trans>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {departures.length > 0 && (
        <section className="flex flex-col gap-3">
          <div className="flex flex-col gap-0.5">
            <h2 className="font-heading text-sm font-medium">
              <Trans>Pending handover</Trans>
            </h2>
            <p className="text-muted-foreground text-[10px]">
              <Trans>
                These people have left the organization and lost access already. What they created
                here is still filed under their name until you hand it over.
              </Trans>
            </p>
          </div>

          <Table className="text-xs">
            <TableHeader>
              <TableRow>
                <TableHead>
                  <Trans>Person</Trans>
                </TableHead>
                <TableHead className="w-32">
                  <Trans>Left</Trans>
                </TableHead>
                <TableHead className="w-20 text-right">
                  <Trans>Files owned</Trans>
                </TableHead>
                <TableHead className="w-40" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {departures.map((departure) => (
                <DepartureRow
                  key={departure.user_id}
                  departure={departure}
                  external={isExternal(departure.email, me?.email)}
                  busy={completeHandover.isPending}
                  onComplete={() => setHandover(departure)}
                />
              ))}
            </TableBody>
          </Table>
        </section>
      )}

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={t`Remove ${removingName} from this space?`}
        description={t`Their access ends immediately. Files they own stay here and wait under Pending handover.`}
        confirmLabel={t`Remove from space`}
        confirmVariant="destructive"
        onConfirm={() => {
          if (!removing) return;
          const memberEmail = removing.email;
          removeMember.mutate(
            { spaceId, userId: removing.user_id },
            {
              onSuccess: () =>
                toast.success(t`Removed ${memberEmail}`, {
                  description: t`Their access ended now. What they left behind waits under Pending handover.`,
                }),
              onError: (e) => toast.error(e.message),
            },
          );
          setRemoving(null);
        }}
      />

      <ConfirmDialog
        open={!!leaver}
        onOpenChange={(open) => !open && setHandover(null)}
        title={t`Complete handover for ${leaverName}?`}
        description={
          owned > 0
            ? plural(owned, {
                one: "This revokes every grant they gave other people, moves anything they created inside someone else’s folder to that folder’s owner, and files the # file they owned at the top level of this space into its Archive.",
                other:
                  "This revokes every grant they gave other people, moves anything they created inside someone else’s folder to that folder’s owner, and files the # files they owned at the top level of this space into its Archive.",
              })
            : t`They owned nothing here, so nothing moves into the Archive. This revokes the grants they gave other people and closes the handover. It cannot be undone.`
        }
        warning={owned > 0 ? t`This cannot be undone.` : undefined}
        confirmLabel={t`Complete handover`}
        confirmVariant={owned > 0 ? "destructive" : "default"}
        onConfirm={() => {
          if (!leaver) return;
          const who = leaverName;
          completeHandover.mutate(leaver.user_id, {
            onSuccess: () => toast.success(t`Handover for ${who} is complete`),
            onError: (e) => toast.error(e.message),
          });
          setHandover(null);
        }}
      />
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

/** Group chips a row shows before folding the rest into "+N". */
const MAX_GROUP_CHIPS = 2;

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
  const { t } = useLingui();
  const f = useFormatters();
  const name = member.display_name || member.name || member.email;
  const memberEmail = member.email;
  const joinable = groups.filter(
    (g) => !g.is_everyone && !g.is_admin && !held.some((h) => h.id === g.id),
  );
  const leavable = held.filter((g) => !g.is_everyone && !g.is_admin);
  const shownGroups = held.slice(0, MAX_GROUP_CHIPS);
  const hiddenGroups = held.length - shownGroups.length;

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
              {isSelf && (
                <Badge variant="secondary">
                  <Trans>You</Trans>
                </Badge>
              )}
              {member.is_admin && (
                <Badge variant="outline">
                  <Trans>Admin</Trans>
                </Badge>
              )}
              {external && <ExternalChip />}
            </span>
            <span className="text-muted-foreground truncate text-[10px]">{member.email}</span>
          </div>
        </div>
      </TableCell>

      <TableCell>
        {/* One line, whatever the count: the first two groups as chips and the
            rest folded into "+N", so every row keeps the same height. The full
            list is in the tooltip and in the row's menu. */}
        <div
          className="flex min-w-0 flex-nowrap items-center gap-1 overflow-hidden"
          title={held.map((g) => g.name).join(", ")}
        >
          {shownGroups.map((g) => (
            <button
              key={g.id}
              type="button"
              className="border-border text-muted-foreground hover:text-foreground inline-flex max-w-28 shrink-0 items-center gap-1 rounded-sm border px-1.5 text-[10px]"
              onClick={() => onOpenGroup(g)}
            >
              {(g.is_everyone || g.is_admin) && <Lock className="size-2.5 shrink-0" />}
              <span className="truncate">{g.name}</span>
            </button>
          ))}
          {hiddenGroups > 0 && (
            <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">
              +{hiddenGroups}
            </span>
          )}
        </div>
      </TableCell>

      <TableCell className="text-muted-foreground tabular-nums">
        {f.date(member.created_at)}
      </TableCell>
      <TableCell className="text-muted-foreground tabular-nums">
        {member.last_seen ? f.relative(member.last_seen) : t`never`}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {member.files_owned.toLocaleString()}
      </TableCell>

      {canManage && (
        <TableCell className="text-right">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-xs" aria-label={t`Actions for ${memberEmail}`} />
              }
            >
              <MoreHorizontal />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              {/* Admin is membership of the automatic Admin group, so this item
                  and that group are the same fact — which is why the group
                  itself is read-only in the drawer. */}
              <DropdownMenuItem disabled={isSelf} onClick={() => onAdminToggle(!member.is_admin)}>
                {member.is_admin ? (
                  <>
                    <ShieldOff /> <Trans>Remove admin rights</Trans>
                  </>
                ) : (
                  <>
                    <ShieldCheck /> <Trans>Make admin</Trans>
                  </>
                )}
              </DropdownMenuItem>
              {joinable.length > 0 && (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <UserPlus /> <Trans>Add to group</Trans>
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {joinable.map((g) => (
                      <DropdownMenuItem key={g.id} onClick={() => onAddToGroup(g)}>
                        {g.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
              {leavable.length > 0 && (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <Users /> <Trans>Remove from group</Trans>
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {leavable.map((g) => (
                      <DropdownMenuItem key={g.id} onClick={() => onRemoveFromGroup(g)}>
                        {g.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" disabled={isSelf} onClick={onRemove}>
                <UserMinus /> <Trans>Remove from space</Trans>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </TableCell>
      )}
    </TableRow>
  );
}

interface DepartureRowProps {
  departure: SpaceDeparture;
  external: boolean;
  busy: boolean;
  onComplete: () => void;
}

/**
 * Somebody the organization dropped. The row is deliberately quiet: the loss
 * of access already happened elsewhere and needs no confirming — the only
 * outstanding decision is the destructive one behind the button.
 */
function DepartureRow({ departure, external, busy, onComplete }: DepartureRowProps) {
  const { i18n } = useLingui();
  const f = useFormatters();
  const name = departure.display_name || departure.name || departure.email;

  return (
    <TableRow className="bg-muted/20">
      <TableCell>
        <div className="flex min-w-0 items-center gap-2">
          <Avatar size="sm" className="shrink-0 grayscale">
            <AvatarImage src={departure.avatar_url} />
            <AvatarFallback>
              <UserRoundX className="size-3" />
            </AvatarFallback>
          </Avatar>
          <div className="flex min-w-0 flex-col leading-tight">
            <span className="flex items-center gap-1 truncate font-medium">
              <span className="truncate">{name}</span>
              {external && <ExternalChip />}
            </span>
            <span className="text-muted-foreground truncate text-[10px]">{departure.email}</span>
          </div>
        </div>
      </TableCell>
      <TableCell className="text-muted-foreground" title={f.date(departure.detected_at)}>
        <span className="block tabular-nums">{f.relative(departure.detected_at)}</span>
        <span className="block text-[10px]">{i18n._(DEPARTURE_SOURCE[departure.source])}</span>
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {departure.files_owned.toLocaleString()}
      </TableCell>
      <TableCell className="text-right">
        <Button variant="outline" size="xs" disabled={busy} onClick={onComplete}>
          <Trans>Complete handover</Trans>
        </Button>
      </TableCell>
    </TableRow>
  );
}

/** Flags an address outside the signed-in user's own mail domain. */
function ExternalChip() {
  const { t } = useLingui();
  return (
    <Badge
      variant="outline"
      className="shrink-0"
      title={t`This address is on a different mail domain from yours`}
    >
      <Trans>External</Trans>
    </Badge>
  );
}

function isExternal(email: string, myEmail?: string): boolean {
  const mine = myEmail?.split("@")[1]?.toLowerCase();
  const theirs = email.split("@")[1]?.toLowerCase();
  return !!mine && !!theirs && mine !== theirs;
}

/** Downloads the rows currently on screen, filters included. */
function exportCsv(members: SpaceMember[], byUser: Map<string, Group[]>) {
  // The CSV is data, not interface: the header row and the `Yes`/`No` cells
  // are the column contract a spreadsheet or an importer reads, so they stay
  // in English however the app is displayed. Translating them would change
  // the file's schema with the reader's language.
  // eslint-disable-next-line lingui/no-unlocalized-strings -- CSV column headers: the file's schema
  const header = "Name,Email,Admin,Groups,Joined,Last active,Files owned".split(",");
  const rows = members.map((m) => [
    m.display_name || m.name || "",
    m.email,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- CSV admin cell, a machine value
    m.is_admin ? "Yes" : "No",
    (byUser.get(m.user_id) ?? []).map((g) => g.name).join(" | "),
    m.created_at,
    m.last_seen ?? "",
    String(m.files_owned),
  ]);

  const csv = [header, ...rows].map((row) => row.map(csvCell).join(",")).join("\r\n");
  const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM tag name
  const a = document.createElement("a");
  a.href = url;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- download filename, matches the English column contract
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
