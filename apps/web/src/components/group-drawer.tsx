import { useState } from "react";
import { toast } from "sonner";
import { FolderLock, Lock, Plus, Trash2, Users, X } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
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
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Textarea } from "@workspace/ui/components/textarea";
import { Avatar, AvatarFallback, AvatarImage } from "@workspace/ui/components/avatar";
import { cn } from "@workspace/ui/lib/utils";
import { ApiError, type Group, type Space } from "@/lib/api";
import { useKeyedState } from "@/lib/use-keyed-state";
import {
  useAddGroupMember,
  useCreateGroup,
  useDeleteGroup,
  useGroupMembers,
  useGroups,
  useRemoveGroupMember,
  useSpaceMembers,
} from "@/lib/queries";
import { GranteePicker, type Grantee } from "@/components/grantee-picker";
import { ConfirmDialog } from "@/components/confirm-dialog";

interface GroupDrawerProps {
  space: Space;
  /** The group being inspected, or undefined for the group list alone. */
  selectedId: string | undefined;
  onSelect: (groupId: string | undefined) => void;
}

/**
 * The right-hand column of the People page: the space's groups, and who is in
 * the one selected.
 *
 * Selection lives in the URL rather than in state, so a group is a place a
 * colleague can be sent to. The list doubles as the table's group filter.
 */
export function GroupDrawer({ space, selectedId, onSelect }: GroupDrawerProps) {
  const { data: groups = [], isLoading } = useGroups(space.id);
  const selected = groups.find((g) => g.id === selectedId);

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h3 className="font-heading text-xs font-medium">Groups</h3>
          <p className="text-muted-foreground text-[10px]">
            Access is given group → folder. Membership of the space opens nothing on its own.
          </p>
        </div>
        {space.is_admin && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="New group"
            onClick={() => void NewGroupModal.show({ spaceId: space.id })}
          >
            <Plus />
          </Button>
        )}
      </div>

      {isLoading && <p className="text-muted-foreground text-xs">Loading groups…</p>}

      <nav className="flex flex-col gap-0.5">
        {groups.map((g) => {
          const active = g.id === selectedId;
          return (
            <button
              key={g.id}
              type="button"
              aria-pressed={active}
              onClick={() => onSelect(active ? undefined : g.id)}
              className={cn(
                "flex w-full items-center gap-1.5 rounded-sm px-2 py-1 text-left text-xs transition-colors",
                active
                  ? "bg-accent text-foreground border-primary border-l-2"
                  : "text-muted-foreground hover:bg-accent/50 hover:text-foreground border-l-2 border-transparent",
              )}
            >
              {(g.is_everyone || g.is_admin) && <Lock className="size-3 shrink-0" />}
              <span className="truncate">{g.name}</span>
              <span className="ml-auto flex shrink-0 items-center gap-2 text-[10px] tabular-nums">
                <span className="flex items-center gap-0.5" title="Members">
                  <Users className="size-3" />
                  {g.member_count}
                </span>
                <span className="flex items-center gap-0.5" title="Folders this group opens">
                  <FolderLock className="size-3" />
                  {g.grant_count}
                </span>
              </span>
            </button>
          );
        })}
        {!isLoading && groups.length === 0 && (
          <p className="text-muted-foreground text-xs">No groups yet.</p>
        )}
      </nav>

      {selected && (
        <GroupMembers space={space} group={selected} onDeleted={() => onSelect(undefined)} />
      )}

      <p className="text-muted-foreground border-border mt-auto rounded-sm border p-2 text-[10px]">
        Groups decide what people can open. Give a group a file or folder from that item’s “Share”
        dialog.
      </p>
    </div>
  );
}

function GroupMembers({
  space,
  group,
  onDeleted,
}: {
  space: Space;
  group: Group;
  onDeleted: () => void;
}) {
  const { data: members = [], isLoading } = useGroupMembers(space.id, group.id);
  // Only this space's people: a group belongs to one company, so the server
  // refuses an outsider anyway (422) — offering them was an error waiting to
  // be clicked.
  const { data: spaceMembers = [] } = useSpaceMembers(space.id);
  const add = useAddGroupMember(space.id);
  const remove = useRemoveGroupMember(space.id);
  const del = useDeleteGroup(space.id);
  const [confirming, setConfirming] = useState(false);

  // The server maintains Everyone and Admin, so their membership is a result,
  // not a setting: Everyone follows the space, Admin follows the switch on the
  // People table.
  const automatic = group.is_everyone || group.is_admin;
  const canEditMembers = space.is_admin && !automatic;

  return (
    <div className="border-border flex flex-col gap-2 border-t pt-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex items-center gap-1.5 text-xs font-medium">
            <span className="truncate">{group.name}</span>
            {automatic && (
              <Badge variant="secondary" className="gap-1 font-normal">
                <Lock className="size-2.5" />
                Automatic
              </Badge>
            )}
          </span>
          <span className="text-muted-foreground text-[10px]">
            {group.member_count} {group.member_count === 1 ? "member" : "members"} · opens{" "}
            {group.grant_count} {group.grant_count === 1 ? "folder" : "folders"}
          </span>
          {group.description && (
            <span className="text-muted-foreground text-[10px]">{group.description}</span>
          )}
        </div>
        {space.is_admin && !automatic && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Delete ${group.name}`}
            onClick={() => setConfirming(true)}
          >
            <Trash2 />
          </Button>
        )}
      </div>

      {group.is_everyone && (
        <p className="text-muted-foreground text-[10px]">
          Everyone in this space belongs to this group, and it updates itself as people join and
          leave. Give it the folders the whole company should see.
        </p>
      )}
      {group.is_admin && (
        <p className="text-muted-foreground text-[10px]">
          This group holds the space admins. Use the Admin switch in the table to change it.
        </p>
      )}

      {canEditMembers && (
        <GranteePicker
          placeholder="Add someone to this group…"
          options={spaceMembers
            .filter((m) => !members.some((existing) => existing.user_id === m.user_id))
            .map<Grantee>((m) => ({
              kind: "user",
              id: m.user_id,
              label: m.display_name || m.name || m.email,
              email: m.email,
              avatarUrl: m.avatar_url || undefined,
            }))}
          onPick={(picked) =>
            add.mutate(
              { groupId: group.id, userId: picked.id },
              {
                onError: (e) =>
                  toast.error(
                    e instanceof ApiError ? e.message : "Could not add them to the group",
                  ),
              },
            )
          }
        />
      )}

      {isLoading && <p className="text-muted-foreground text-[10px]">Loading…</p>}
      {!isLoading && members.length === 0 && (
        <p className="text-muted-foreground text-[10px]">
          Nobody in this group yet — it opens nothing for anyone.
        </p>
      )}

      <div className="flex flex-col gap-0.5">
        {members.map((m) => {
          const name = m.display_name || m.name || m.email;
          return (
            <div key={m.user_id} className="flex items-center gap-2 py-0.5">
              <Avatar size="sm" className="shrink-0">
                <AvatarImage src={m.avatar_url} />
                <AvatarFallback>{name.slice(0, 2).toUpperCase()}</AvatarFallback>
              </Avatar>
              <span className="min-w-0 truncate text-xs">{name}</span>
              {canEditMembers && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="ml-auto"
                  aria-label={`Remove ${name} from ${group.name}`}
                  onClick={() => remove.mutate({ groupId: group.id, userId: m.user_id })}
                >
                  <X />
                </Button>
              )}
            </div>
          );
        })}
      </div>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Delete "${group.name}"?`}
        // The count is the point: the grants cascade away with the group, and
        // the people in it lose that access with no other notice.
        description={
          group.grant_count > 0
            ? `${group.member_count} ${group.member_count === 1 ? "person" : "people"} will lose access to ${group.grant_count} ${group.grant_count === 1 ? "folder" : "folders"}. This cannot be undone.`
            : "This group opens no folders, so nobody loses access."
        }
        confirmLabel="Delete group"
        confirmVariant="destructive"
        onConfirm={() => {
          del.mutate(group.id, {
            onSuccess: (r) => {
              toast.success(
                r.grants_revoked > 0
                  ? `Deleted "${group.name}" and revoked ${r.grants_revoked} ${r.grants_revoked === 1 ? "grant" : "grants"}`
                  : `Deleted "${group.name}"`,
              );
              onDeleted();
            },
            onError: (e) => toast.error(e instanceof ApiError ? e.message : "Could not delete"),
          });
          setConfirming(false);
        }}
      />
    </div>
  );
}

const NewGroupModal = createModal<{ spaceId: string }>("new-group", ({ spaceId, modal }) => {
  // better-modal leaves a closed modal mounted, so plain `useState` would
  // show the previously created group's name the next time this opens.
  const opening = `${spaceId}:${modal.visible}`;
  const [name, setName] = useKeyedState(opening, "");
  const [description, setDescription] = useKeyedState(opening, "");
  const create = useCreateGroup(spaceId);

  return (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New group</DialogTitle>
          <DialogDescription>
            Name it after the people, not the folder — one group usually opens several.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              { name, description },
              {
                onSuccess: () => {
                  toast.success(`Created "${name.trim()}"`);
                  modal.resolve(undefined);
                },
                onError: (e) =>
                  toast.error(e instanceof ApiError ? e.message : "Could not create the group"),
              },
            );
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="group-name">Name</Label>
            <Input
              id="group-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Finance"
              autoFocus
              required
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="group-description">Description</Label>
            <Textarea
              id="group-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Who belongs in here, and why"
              rows={2}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => modal.resolve(undefined)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              Create group
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
});
