import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { Lock, ShieldCheck, Users, X } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
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
import { ApiError, type Node } from "@/lib/api";
import { useGrants, useGroups, useRevokeGrant, useUpsertGrant } from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = { node: Node; spaceId: string };

// base-ui's SelectValue renders the raw value unless the Select is given an
// `items` list to map value → label with, which is why every Select in this app
// passes one. Without it the trigger showed a group's uuid.
const LEVEL_ITEMS: { value: "view" | "edit"; label: string }[] = [
  { value: "view", label: "View" },
  { value: "edit", label: "Edit" },
];

/**
 * Who can open a folder, and the one place to change it.
 *
 * It answers both questions at once on purpose. Revoking access without being
 * able to see who currently holds it is guesswork, and the whole point of
 * groups is that access is legible — every line here is one grant row.
 *
 * Access inherits downward, so a grant made here also opens everything inside.
 * Granting needs the share capability on this folder — its owner has it, and so
 * does anyone a grant handed it to. Being a space admin does not.
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
    const [level, setLevel] = useKeyedState<string, "view" | "edit">(node.id, "view");
    const [canDownload, setCanDownload] = useKeyedState<string, boolean>(node.id, false);
    const [canShare, setCanShare] = useKeyedState<string, boolean>(node.id, false);

    const granted = new Set(grants.map((g) => g.group_id));
    const available = groups.filter((g) => !granted.has(g.id));
    const groupItems = available.map((g) => ({ label: g.name, value: g.id }));

    function add() {
      if (!groupId) return;
      // An editor can always download what it edits — the database says so,
      // and sending a contradiction would just come back as a constraint
      // violation.
      const caps = {
        level,
        can_download: level === "edit" ? true : canDownload,
        can_share: canShare,
      };
      grant.mutate(
        { groupId, caps },
        {
          onSuccess: () => {
            const name = groups.find((g) => g.id === groupId)?.name ?? "group";
            toast.success(`${name} can now ${level} “${node.name}”`);
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
                    No group opens this folder — only you and anyone you shared it with directly can
                    see it.
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
                    <span className="text-muted-foreground ml-auto text-xs whitespace-nowrap">
                      {[g.level, g.can_download && "download", g.can_share && "re-share"]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
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
                  value={level}
                  items={LEVEL_ITEMS}
                  onValueChange={(v) => setLevel(v as "view" | "edit")}
                >
                  <SelectTrigger size="sm" className="w-28" aria-label="Access level to give">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {LEVEL_ITEMS.map((p) => (
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
              <div className="flex flex-wrap items-center gap-4 pt-1">
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="grant-download"
                    checked={level === "edit" ? true : canDownload}
                    disabled={level === "edit"}
                    onCheckedChange={(v) => setCanDownload(Boolean(v))}
                  />
                  <Label htmlFor="grant-download" className="text-xs font-normal">
                    Allow download
                    {level === "edit" && (
                      <span className="text-muted-foreground ml-1">(included with edit)</span>
                    )}
                  </Label>
                </div>
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="grant-share"
                    checked={canShare}
                    onCheckedChange={(v) => setCanShare(Boolean(v))}
                  />
                  <Label htmlFor="grant-share" className="text-xs font-normal">
                    Allow re-sharing
                  </Label>
                </div>
              </div>
            </div>

            <p className="text-muted-foreground flex items-start gap-2 text-xs">
              <ShieldCheck className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              Nobody else reaches this folder — not even a space admin — unless a grant here, or a
              share you made, says so.
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
