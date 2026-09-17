import { useState } from "react";
import { toast } from "sonner";
import { ChevronRight, FolderLock, Lock, Plus, Trash2, Users, X } from "lucide-react";
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
import { ApiError, type Group, type Space } from "@/lib/api";
import {
  useAddGroupMember,
  useCreateGroup,
  useDeleteGroup,
  useGroupMembers,
  useGroups,
  useRemoveGroupMember,
} from "@/lib/queries";
import { MemberPicker } from "@/components/member-picker";
import { ConfirmDialog } from "@/components/confirm-dialog";

/**
 * Groups are how anyone other than an admin reaches a file: a role grants no
 * access on its own, so access is given group → folder. This screen manages the
 * groups; the grants themselves are set on a folder, from its ⋯ menu.
 */
export function SpaceGroupsPanel({ space }: { space: Space }) {
  const { data: groups = [], isLoading } = useGroups(space.id);
  const [openId, setOpenId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h3 className="font-heading text-sm font-medium">Groups</h3>
          <p className="text-muted-foreground max-w-prose text-xs">
            A group is a set of people you give access to folders. Being a member of this space does
            not open any file on its own — what someone can see is what their groups have been
            given.
          </p>
        </div>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" />
          New group
        </Button>
      </div>

      {isLoading && <p className="text-muted-foreground text-xs">Loading groups…</p>}

      <div className="flex flex-col gap-px overflow-hidden rounded-md border">
        {groups.map((g) => (
          <GroupRow
            key={g.id}
            space={space}
            group={g}
            open={openId === g.id}
            onToggle={() => setOpenId(openId === g.id ? null : g.id)}
          />
        ))}
        {!isLoading && groups.length === 0 && (
          <p className="text-muted-foreground p-4 text-center text-xs">No groups yet.</p>
        )}
      </div>

      <NewGroupDialog space={space} open={creating} onOpenChange={setCreating} />
    </div>
  );
}

function GroupRow({
  space,
  group,
  open,
  onToggle,
}: {
  space: Space;
  group: Group;
  open: boolean;
  onToggle: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const del = useDeleteGroup(space.id);

  // Automatic groups (Everyone and Admin) cannot be deleted or renamed
  const isAutomatic = group.is_everyone || group.is_admin;

  return (
    <div className="bg-background">
      <div className="hover:bg-muted/40 flex items-center gap-3 px-3 py-2.5">
        <button
          type="button"
          onClick={onToggle}
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
          aria-expanded={open}
        >
          <ChevronRight
            className={`text-muted-foreground h-4 w-4 shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <div className="flex min-w-0 flex-col">
            <span className="flex items-center gap-2 text-sm font-medium">
              {group.name}
              {isAutomatic && (
                <Badge variant="secondary" className="gap-1 font-normal">
                  <Lock className="h-3 w-3" />
                  Automatic
                </Badge>
              )}
            </span>
            {group.description && (
              <span className="text-muted-foreground truncate text-xs">{group.description}</span>
            )}
          </div>
        </button>

        <span className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
          <Users className="h-3.5 w-3.5" />
          {group.member_count}
        </span>
        <span
          className="text-muted-foreground flex w-20 shrink-0 items-center gap-1 text-xs"
          title="Folders this group opens"
        >
          <FolderLock className="h-3.5 w-3.5" />
          {group.grant_count}
        </span>

        {!isAutomatic && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Delete ${group.name}`}
            onClick={() => setConfirming(true)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>

      {open && <GroupMembers space={space} group={group} />}

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
        onConfirm={() => {
          del.mutate(group.id, {
            onSuccess: (r) =>
              toast.success(
                r.grants_revoked > 0
                  ? `Deleted "${group.name}" and revoked ${r.grants_revoked} ${r.grants_revoked === 1 ? "grant" : "grants"}`
                  : `Deleted "${group.name}"`,
              ),
            onError: (e) => toast.error(e instanceof ApiError ? e.message : "Could not delete"),
          });
          setConfirming(false);
        }}
      />
    </div>
  );
}

function GroupMembers({ space, group }: { space: Space; group: Group }) {
  const { data: members = [], isLoading } = useGroupMembers(space.id, group.id);
  const add = useAddGroupMember(space.id);
  const remove = useRemoveGroupMember(space.id);

  // Automatic groups (Everyone and Admin) have read-only membership
  const isAutomatic = group.is_everyone || group.is_admin;

  return (
    <div className="bg-muted/30 border-t px-3 py-3 pl-10">
      {group.is_everyone ? (
        <p className="text-muted-foreground text-xs">
          Everyone in this space belongs to this group, and it updates itself as people join and
          leave. Give it access to the folders the whole company should see.
        </p>
      ) : group.is_admin ? (
        <p className="text-muted-foreground text-xs">
          Admin membership is managed in the Members tab. This group contains all space admins.
        </p>
      ) : (
        <div className="mb-3 max-w-xs">
          <MemberPicker
            placeholder="Add someone to this group…"
            excludeIds={members.map((m) => m.user_id)}
            onSelect={(user) =>
              add.mutate(
                { groupId: group.id, userId: user.id },
                {
                  onError: (e) =>
                    toast.error(
                      e instanceof ApiError ? e.message : "Could not add them to the group",
                    ),
                },
              )
            }
          />
        </div>
      )}

      {isLoading && <p className="text-muted-foreground text-xs">Loading…</p>}
      {!isLoading && members.length === 0 && (
        <p className="text-muted-foreground text-xs">
          Nobody in this group yet — it opens nothing for anyone.
        </p>
      )}

      <div className="flex flex-col gap-1">
        {members.map((m) => (
          <div key={m.user_id} className="flex items-center gap-2 py-1">
            <Avatar className="h-6 w-6">
              <AvatarImage src={m.avatar_url} />
              <AvatarFallback className="text-[10px]">
                {(m.display_name || m.name || m.email).slice(0, 2).toUpperCase()}
              </AvatarFallback>
            </Avatar>
            <span className="text-sm">{m.display_name || m.name || m.email}</span>
            <span className="text-muted-foreground truncate text-xs">{m.email}</span>
            {!isAutomatic && (
              <Button
                variant="ghost"
                size="icon-sm"
                className="ml-auto"
                aria-label={`Remove ${m.display_name || m.name || m.email} from ${group.name}`}
                onClick={() => remove.mutate({ groupId: group.id, userId: m.user_id })}
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function NewGroupDialog({
  space,
  open,
  onOpenChange,
}: {
  space: Space;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const create = useCreateGroup(space.id);

  function submit() {
    create.mutate(
      { name, description },
      {
        onSuccess: () => {
          toast.success(`Created "${name.trim()}"`);
          setName("");
          setDescription("");
          onOpenChange(false);
        },
        onError: (e) =>
          toast.error(e instanceof ApiError ? e.message : "Could not create the group"),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New group</DialogTitle>
          <DialogDescription>
            Name it after the people, not the folder — one group usually opens several.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="group-name">Name</Label>
            <Input
              id="group-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Finance"
              autoFocus
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
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim() || create.isPending}>
            Create group
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
