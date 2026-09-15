import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { Lock, ShieldCheck, Users, X } from "lucide-react";
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
import { Label } from "@workspace/ui/components/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { ApiError, type Node, type SharePerm } from "@/lib/api";
import { useGrants, useGroups, useRevokeGrant, useUpsertGrant } from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = { node: Node; spaceId: string };

// base-ui's SelectValue renders the raw value unless the Select is given an
// `items` list to map value → label with, which is why every Select in this app
// passes one. Without it the trigger showed a group's uuid.
const PERM_ITEMS: { value: SharePerm; label: string }[] = [
  { value: "view", label: "View" },
  { value: "download", label: "Download" },
  { value: "edit", label: "Edit" },
];

const PERMS: { value: SharePerm; label: string; blurb: string }[] = [
  { value: "view", label: "View", blurb: "Open and read what is inside" },
  { value: "download", label: "Download", blurb: "View, plus save copies to their machine" },
  { value: "edit", label: "Edit", blurb: "Add, change, move and delete things inside" },
];

/**
 * Who can open a folder, and the one place to change it.
 *
 * It answers both questions at once on purpose. Revoking access without being
 * able to see who currently holds it is guesswork, and the whole point of
 * groups is that access is legible — every line here is one grant row.
 *
 * Access inherits downward, so a grant made here also opens everything inside.
 * Grants are admin-only; a member with edit can still share a link.
 */
export const FolderAccessDialog = createModal<Props, null>(
  "folder-access",
  ({ node, spaceId, modal }) => {
    const { data: grants = [], isLoading } = useGrants(modal.visible ? node.id : null);
    const { data: groups = [] } = useGroups(modal.visible ? spaceId : null);
    const grant = useUpsertGrant(node.id);
    const revoke = useRevokeGrant(node.id);

    // Keyed to the folder: better-modal keeps a closed modal mounted, so plain
    // useState would carry the previous folder's picks into the next opening
    // and could grant a group access to something nobody chose.
    const [groupId, setGroupId] = useKeyedState<string, string>(node.id, "");
    const [perm, setPerm] = useKeyedState<string, SharePerm>(node.id, "view");

    const granted = new Set(grants.map((g) => g.group_id));
    const available = groups.filter((g) => !granted.has(g.id));
    const groupItems = available.map((g) => ({ label: g.name, value: g.id }));

    function add() {
      if (!groupId) return;
      grant.mutate(
        { groupId, perm },
        {
          onSuccess: () => {
            const name = groups.find((g) => g.id === groupId)?.name ?? "group";
            toast.success(`${name} can now ${perm} "${node.name}"`);
            setGroupId("");
          },
          onError: (e) => toast.error(e instanceof ApiError ? e.message : "Could not grant access"),
        },
      );
    }

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Who can open “{node.name}”</DialogTitle>
            <DialogDescription>
              Access given here also opens everything inside this folder.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <Label className="text-xs">Groups with access</Label>
              <div className="flex flex-col divide-y rounded-md border">
                {isLoading && <p className="text-muted-foreground p-3 text-xs">Loading access…</p>}
                {!isLoading && grants.length === 0 && (
                  <p className="text-muted-foreground p-3 text-xs">
                    No group opens this folder. Only space admins can see it.
                  </p>
                )}
                {grants.map((g) => (
                  <div key={g.id} className="flex items-center gap-2 px-3 py-2">
                    <Users className="text-muted-foreground h-4 w-4 shrink-0" />
                    <span className="truncate text-sm">{g.group_name}</span>
                    {g.is_everyone && (
                      <Badge variant="secondary" className="gap-1 font-normal">
                        <Lock className="h-3 w-3" />
                        Everyone
                      </Badge>
                    )}
                    <Badge variant="outline" className="ml-auto font-normal capitalize">
                      {g.perm}
                    </Badge>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove ${g.group_name}'s access`}
                      onClick={() =>
                        revoke.mutate(g.group_id, {
                          onSuccess: () => toast.success(`${g.group_name} no longer has access`),
                          onError: (e) =>
                            toast.error(e instanceof ApiError ? e.message : "Could not revoke"),
                        })
                      }
                    >
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label className="text-xs">Give a group access</Label>
              <div className="flex gap-2">
                <Select
                  value={groupId}
                  items={groupItems}
                  onValueChange={(v) => setGroupId(v as string)}
                >
                  <SelectTrigger size="sm" className="flex-1" aria-label="Group to give access to">
                    <SelectValue
                      placeholder={available.length ? "Choose a group…" : "No groups left"}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {available.map((g) => (
                      <SelectItem key={g.id} value={g.id}>
                        {g.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={perm}
                  items={PERM_ITEMS}
                  onValueChange={(v) => setPerm(v as SharePerm)}
                >
                  <SelectTrigger size="sm" className="w-32" aria-label="Permission to give">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PERMS.map((p) => (
                      <SelectItem key={p.value} value={p.value}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button size="sm" onClick={add} disabled={!groupId || grant.isPending}>
                  Give access
                </Button>
              </div>
              <p className="text-muted-foreground text-xs">
                {PERMS.find((p) => p.value === perm)?.blurb}
              </p>
            </div>

            <p className="text-muted-foreground flex items-start gap-2 text-xs">
              <ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              Space admins can open every folder here, whether or not a group grants it.
            </p>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => modal.resolve(null)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
