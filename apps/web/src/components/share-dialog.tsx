import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { Copy, Link2, Lock, ShieldCheck, Users, X } from "lucide-react";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Separator } from "@workspace/ui/components/separator";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import { ApiError, type GrantCaps, type Node, type Share } from "@/lib/api";
import {
  useCreateShare,
  useGrants,
  useGroups,
  useMe,
  useRevokeGrant,
  useRevokeShare,
  useShares,
  useSpaceMembers,
  useUpsertGrant,
} from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";
import { GranteePicker, type Grantee } from "@/components/grantee-picker";

type Props = { node: Node };

const message = (e: unknown, fallback: string) =>
  e instanceof ApiError ? e.message : ((e as Error)?.message ?? fallback);

/**
 * Who can reach one node, and the one place to change it.
 *
 * Access is one table — `shares`, keyed by kind — so it is answered by one
 * dialog. Splitting it in two (people-and-link here, groups in a folder-only
 * "Who can open this…") meant neither surface could show the whole answer: a
 * group grant showed up in the share list as a row labelled with the literal
 * word "group", and a file could not be given to a group at all even though
 * the server has always allowed it.
 *
 * Grantees come from the space, never from a tenant-wide directory: a space is
 * a company, and the picker must not double as an address book for all of them.
 */
export const ShareDialog = createModal<Props, null>("share", ({ node, modal }) => {
  const live = modal.visible;
  const me = useMe();
  const { data: groups = [] } = useGroups(live ? node.space_id : null);
  const { data: members = [] } = useSpaceMembers(live ? node.space_id : null);
  const grants = useGrants(live ? node.id : null);
  const shares = useShares(live ? node.id : null);

  const upsertGrant = useUpsertGrant(node.id);
  const revokeGrant = useRevokeGrant(node.id);
  const createShare = useCreateShare();
  const revokeShare = useRevokeShare();

  // Keyed to the node: better-modal keeps a closed modal mounted, so plain
  // useState would carry the previous item's picks into the next opening and
  // could hand access to something nobody chose.
  const [picked, setPicked] = useKeyedState<string, Grantee[]>(node.id, []);
  const [level, setLevel] = useKeyedState<string, "view" | "edit">(node.id, "view");
  const [canDownload, setCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [canShare, setCanShare] = useKeyedState<string, boolean>(node.id, false);
  const [linkCanDownload, setLinkCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [expires, setExpires] = useKeyedState<string, string>(node.id, "0");

  const directShares = (shares.data ?? []).filter((s) => s.kind === "direct");
  const linkShares = (shares.data ?? []).filter((s) => s.kind === "link");
  // Group rows come back from GET /shares too, without a grantee this shape
  // could name. /grants is the labelled view of the same rows.
  const grantRows = grants.data ?? [];

  const pickedIds = new Set(picked.map((g) => g.id));
  const grantedGroups = new Set(grantRows.map((g) => g.group_id));
  const sharedEmails = new Set(directShares.map((s) => s.grantee_email));

  const options: Grantee[] = [
    ...groups
      .filter((g) => !grantedGroups.has(g.id) && !pickedIds.has(g.id))
      .map<Grantee>((g) => ({
        kind: "group",
        id: g.id,
        label: g.name,
        isEveryone: g.is_everyone,
        isAdmin: g.is_admin,
      })),
    ...members
      // Sharing with yourself is refused by the server, and re-sharing with
      // someone who already holds a grant would only collide: both belong out
      // of the list rather than in an error toast.
      .filter(
        (m) => m.user_id !== me.data?.id && !sharedEmails.has(m.email) && !pickedIds.has(m.user_id),
      )
      .map<Grantee>((m) => ({
        kind: "user",
        id: m.user_id,
        label: m.display_name || m.name || m.email,
        email: m.email,
        avatarUrl: m.avatar_url || undefined,
      })),
  ];

  // Groups worth one click: the quick row above the picker, so giving a whole
  // team access does not need a search first.
  const quickGroups = options.filter(
    (g): g is Extract<Grantee, { kind: "group" }> => g.kind === "group",
  );

  // An editor can always download what it edits — the database says so, and
  // sending the contradiction would come back as a constraint violation.
  // Re-sharing stays independent of level: a viewer may pass their view on,
  // and an editor may be denied it.
  const caps: GrantCaps = {
    level,
    can_download: level === "edit" ? true : canDownload,
    can_share: canShare,
  };
  const busy = upsertGrant.isPending || createShare.isPending;

  async function submit() {
    if (picked.length === 0) return;
    const failed: string[] = [];
    let firstError = "";
    for (const g of picked) {
      try {
        if (g.kind === "group") {
          await upsertGrant.mutateAsync({ groupId: g.id, caps });
        } else {
          await createShare.mutateAsync({
            nodeId: node.id,
            body: { kind: "direct", email: g.email, ...caps },
          });
        }
      } catch (e) {
        failed.push(g.label);
        firstError ||= message(e, "Could not share");
      }
    }
    const shared = picked.length - failed.length;
    if (shared > 0) {
      toast.success(`${shared === 1 ? picked[0].label : `${shared} grantees`} can now ${level}`);
    }
    if (failed.length > 0) {
      toast.error(`${failed.join(", ")}: ${firstError}`);
    }
    setPicked(failed.length > 0 ? picked.filter((g) => failed.includes(g.label)) : []);
  }

  function createLink() {
    createShare.mutate(
      {
        nodeId: node.id,
        body: {
          kind: "link",
          level: "view",
          can_download: linkCanDownload,
          can_share: false,
          expires_in: Number(expires) || 0,
        },
      },
      {
        onSuccess: (sh: Share) => {
          if (!sh.url) return;
          toast.success("Link created (shown once)", {
            action: { label: "Copy", onClick: () => void navigator.clipboard.writeText(sh.url!) },
          });
        },
        onError: (e) => toast.error(message(e, "Could not create link")),
      },
    );
  }

  const capsLabel = (c: GrantCaps) =>
    [c.level, c.can_download && "download", c.can_share && "re-share"].filter(Boolean).join(" · ");

  return (
    <Dialog open={live} onOpenChange={(open) => !open && modal.resolve(null)}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Share “{node.name}”</DialogTitle>
          <DialogDescription>
            {node.kind === "dir"
              ? "Access given here also opens everything inside this folder."
              : "Give a group or a colleague access to this file."}
          </DialogDescription>
        </DialogHeader>

        <section className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
          <div>
            <h3 className="text-sm font-semibold">Give access</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Groups and people come from this space. The access below applies to everyone picked.
            </p>
          </div>

          {quickGroups.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {quickGroups.map((g) => (
                <Button
                  key={g.id}
                  variant="outline"
                  size="sm"
                  className="h-7 gap-1.5 font-normal"
                  onClick={() => setPicked([...picked, g])}
                >
                  <Users className="h-3.5 w-3.5" />
                  {g.label}
                  {g.isEveryone && <Lock className="h-3 w-3 opacity-60" />}
                  {g.isAdmin && <ShieldCheck className="h-3 w-3 opacity-60" />}
                </Button>
              ))}
            </div>
          )}

          {picked.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {picked.map((g) => (
                <Badge key={g.id} variant="secondary" className="gap-1 py-1 font-normal">
                  {g.kind === "group" ? <Users className="h-3 w-3" /> : null}
                  {g.label}
                  <button
                    type="button"
                    aria-label={`Remove ${g.label}`}
                    onClick={() => setPicked(picked.filter((p) => p.id !== g.id))}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
            </div>
          )}

          <GranteePicker options={options} onPick={(g) => setPicked([...picked, g])} />

          <div className="flex flex-wrap items-end gap-4">
            <div className="grid gap-1.5">
              <Label id="share-level-label" className="text-xs">
                Access level
              </Label>
              <ToggleGroup
                aria-labelledby="share-level-label"
                size="sm"
                variant="outline"
                value={[level]}
                onValueChange={(val) => val.length > 0 && setLevel(val[0] as "view" | "edit")}
              >
                <ToggleGroupItem value="view">Can view</ToggleGroupItem>
                <ToggleGroupItem value="edit">Can edit</ToggleGroupItem>
              </ToggleGroup>
            </div>
            <div className="flex flex-col gap-2 pb-1">
              <div className="flex items-center gap-2">
                <Checkbox
                  id="share-download"
                  checked={level === "edit" ? true : canDownload}
                  disabled={level === "edit"}
                  onCheckedChange={(v) => setCanDownload(Boolean(v))}
                />
                <Label htmlFor="share-download" className="text-xs font-normal">
                  Allow download
                  {level === "edit" && (
                    <span className="text-muted-foreground ml-1">(included with edit)</span>
                  )}
                </Label>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="share-reshare"
                  checked={canShare}
                  onCheckedChange={(v) => setCanShare(Boolean(v))}
                />
                <Label htmlFor="share-reshare" className="text-xs font-normal">
                  Allow re-sharing
                </Label>
              </div>
            </div>
            <Button
              className="ml-auto"
              size="sm"
              disabled={picked.length === 0 || busy}
              onClick={() => void submit()}
            >
              Share
            </Button>
          </div>
        </section>

        <Separator />

        <section className="grid gap-2">
          <div>
            <h3 className="text-sm font-semibold">Current access</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Nobody else reaches this — not even a space admin — unless a line here says so.
            </p>
          </div>

          <div className="bg-background rounded-md border">
            {grants.isPending || shares.isPending ? (
              <p className="text-muted-foreground p-3 text-sm">Loading access…</p>
            ) : grantRows.length + directShares.length + linkShares.length === 0 ? (
              <p className="text-muted-foreground p-3 text-sm">
                No one else has access yet — only you, and anyone who can reach a folder above this.
              </p>
            ) : (
              <ul className="divide-y">
                {grantRows.map((g) => (
                  <li key={g.id} className="flex items-center gap-3 p-3 text-sm">
                    <Users className="text-muted-foreground size-4 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-2 truncate font-medium">
                        {g.group_name}
                        {g.is_everyone && (
                          <Badge variant="secondary" className="gap-1 font-normal">
                            <Lock className="h-3 w-3" />
                            Everyone
                          </Badge>
                        )}
                        {g.is_admin && (
                          <Badge variant="secondary" className="gap-1 font-normal">
                            <ShieldCheck className="h-3 w-3" />
                            Admins
                          </Badge>
                        )}
                      </p>
                      <p className="text-muted-foreground text-[10px] uppercase">Group</p>
                    </div>
                    <span className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">
                      {capsLabel(g)}
                    </span>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="shrink-0"
                      aria-label={`Remove ${g.group_name}'s access`}
                      onClick={() =>
                        revokeGrant.mutate(g.group_id, {
                          onSuccess: () => toast.success(`${g.group_name} no longer has access`),
                          onError: (e) => toast.error(message(e, "Could not revoke")),
                        })
                      }
                    >
                      <X />
                    </Button>
                  </li>
                ))}
                {[...directShares, ...linkShares].map((s) => (
                  <li key={s.id} className="flex items-center gap-3 p-3 text-sm">
                    <Avatar size="sm">
                      {s.kind === "direct" ? (
                        <AvatarFallback>
                          {(s.grantee_email ?? "?").charAt(0).toUpperCase()}
                        </AvatarFallback>
                      ) : (
                        <AvatarFallback>
                          <Link2 className="size-3.5" />
                        </AvatarFallback>
                      )}
                    </Avatar>
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-medium">
                        {s.kind === "link" ? "Anyone with the link" : s.grantee_email}
                      </p>
                      <p className="text-muted-foreground text-[10px] uppercase">
                        {s.kind === "link" ? "Public link" : "Direct share"}
                      </p>
                    </div>
                    <span className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">
                      {capsLabel(s)}
                    </span>
                    {s.url && (
                      <a
                        href={s.url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-primary inline-flex shrink-0 items-center gap-1 text-xs underline"
                      >
                        <Copy className="size-3.5" /> Open
                      </a>
                    )}
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="shrink-0"
                      aria-label={`Revoke access for ${s.grantee_email ?? "the public link"}`}
                      onClick={() =>
                        revokeShare.mutate(
                          { nodeId: node.id, shareId: s.id },
                          {
                            onSuccess: () => toast.success("Access revoked"),
                            onError: (e) => toast.error(message(e, "Could not revoke")),
                          },
                        )
                      }
                    >
                      <X />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>

        <Separator />

        <section className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
          <div>
            <h3 className="text-sm font-semibold">Public link</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Anyone holding the link can view this item without signing in. A link never carries
              editing, and nobody can re-share it onward — download is the one choice it offers.
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="link-download-toggle" className="text-xs">
                Access
              </Label>
              <div className="flex items-center gap-2 pt-1.5">
                <Checkbox
                  id="link-download-toggle"
                  checked={linkCanDownload}
                  onCheckedChange={(v) => setLinkCanDownload(Boolean(v))}
                />
                <Label htmlFor="link-download-toggle" className="text-xs font-normal">
                  Allow download
                </Label>
              </div>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="link-expiry" className="text-xs">
                Expiration
              </Label>
              <Input
                id="link-expiry"
                type="number"
                min={0}
                value={expires}
                onChange={(e) => setExpires(e.target.value)}
                aria-describedby="link-expiry-help"
              />
              <span id="link-expiry-help" className="text-muted-foreground text-xs">
                Seconds; use 0 for no expiration.
              </span>
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="self-start"
            disabled={createShare.isPending}
            onClick={createLink}
          >
            Create public link
          </Button>
        </section>
      </DialogContent>
    </Dialog>
  );
});
