import { createModal } from "@buiducnhat/better-modal";
import { Plural, Select, Trans, useLingui } from "@lingui/react/macro";
import { plural } from "@lingui/core/macro";
import { toast } from "sonner";
import { Copy, Link2, Lock, ShieldCheck, Users, X } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@workspace/ui/components/alert-dialog";
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

/**
 * The server's problem+json message when there is one, else whatever the
 * failure carried. `fallback` arrives already translated: only the caller
 * knows which act failed.
 */
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
  const { t } = useLingui();
  // Named local so the placeholder reaches a translator as `{nodeName}`.
  const nodeName = node.name;
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
  // eslint-disable-next-line lingui/no-unlocalized-strings -- access-level enum from the API
  const [level, setLevel] = useKeyedState<string, "view" | "edit">(node.id, "view");
  const [canDownload, setCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [canShare, setCanShare] = useKeyedState<string, boolean>(node.id, false);
  const [linkCanDownload, setLinkCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [expires, setExpires] = useKeyedState<string, string>(node.id, "0");
  // The row awaiting a revoke confirmation. Keyed like the rest: a closed
  // better-modal stays mounted, and a pending share left over from the last
  // node would offer to revoke access to something else entirely.
  const [pendingRevoke, setPendingRevoke] = useKeyedState<string, Share | null>(node.id, null);

  const directShares: Share[] = [];
  const linkShares: Share[] = [];
  for (const s of shares.data ?? []) {
    if (s.kind === "direct") directShares.push(s);
    else if (s.kind === "link") linkShares.push(s);
  }
  // Group rows come back from GET /shares too, without a grantee this shape
  // could name. /grants is the labelled view of the same rows.
  const grantRows = grants.data ?? [];
  // A team folder inverts the rule the rest of this dialog is written around.
  // Everywhere else ownership outlives a grant, so revoking leaves the
  // grantee's own uploads behind; inside a team folder nothing is personally
  // owned, so a revoke genuinely takes the access away. Every "stays theirs"
  // reassurance below is false here and is dropped rather than reworded.
  const inTeam = node.in_team ?? false;

  const pickedIds = new Set(picked.map((g) => g.id));
  const grantedGroups = new Set(grantRows.map((g) => g.group_id));
  const sharedEmails = new Set(directShares.map((s) => s.grantee_email));

  const options: Grantee[] = [];
  // Groups worth one click: the quick row above the picker, so giving a whole
  // team access does not need a search first.
  const quickGroups: Extract<Grantee, { kind: "group" }>[] = [];
  for (const g of groups) {
    if (grantedGroups.has(g.id) || pickedIds.has(g.id)) continue;
    const grantee: Extract<Grantee, { kind: "group" }> = {
      kind: "group",
      id: g.id,
      label: g.name,
      isEveryone: g.is_everyone,
      isAdmin: g.is_admin,
    };
    options.push(grantee);
    quickGroups.push(grantee);
  }
  for (const m of members) {
    // Sharing with yourself is refused by the server, and re-sharing with
    // someone who already holds a grant would only collide: both belong out
    // of the list rather than in an error toast.
    if (m.user_id === me.data?.id || sharedEmails.has(m.email) || pickedIds.has(m.user_id)) {
      continue;
    }
    options.push({
      // eslint-disable-next-line lingui/no-unlocalized-strings -- grantee kind discriminator
      kind: "user",
      id: m.user_id,
      label: m.display_name || m.name || m.email,
      email: m.email,
      avatarUrl: m.avatar_url || undefined,
    });
  }

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
    const results = await Promise.allSettled(
      picked.map((g) =>
        g.kind === "group"
          ? upsertGrant.mutateAsync({ groupId: g.id, caps })
          : createShare.mutateAsync({
              nodeId: node.id,
              // eslint-disable-next-line lingui/no-unlocalized-strings -- share kind discriminator
              body: { kind: "direct", email: g.email, ...caps },
            }),
      ),
    );
    const failed: string[] = [];
    let firstError = "";
    results.forEach((r, i) => {
      if (r.status === "rejected") {
        failed.push(picked[i].label);
        firstError ||= message(r.reason, t`Could not share`);
      }
    });
    const shared = picked.length - failed.length;
    if (shared > 0) {
      // One whole sentence per level rather than a verb spliced onto a
      // subject: "can now view" does not survive being assembled elsewhere.
      const who =
        shared === 1
          ? picked[0].label
          : t`${plural(shared, { one: "# grantee", other: "# grantees" })}`;
      toast.success(level === "edit" ? t`${who} can now edit` : t`${who} can now view`);
    }
    if (failed.length > 0) {
      // Names and a server message, no phrasing of our own to translate.
      toast.error(`${failed.join(", ")}: ${firstError}`);
    }
    setPicked(failed.length > 0 ? picked.filter((g) => failed.includes(g.label)) : []);
  }

  function createLink() {
    createShare.mutate(
      {
        nodeId: node.id,
        body: {
          // eslint-disable-next-line lingui/no-unlocalized-strings -- share kind discriminator
          kind: "link",
          // eslint-disable-next-line lingui/no-unlocalized-strings -- access-level enum from the API
          level: "view",
          can_download: linkCanDownload,
          can_share: false,
          expires_in: Number(expires) || 0,
        },
      },
      {
        onSuccess: (sh: Share) => {
          if (!sh.url) return;
          toast.success(t`Link created (shown once)`, {
            action: {
              label: t`Copy`,
              onClick: () => void navigator.clipboard.writeText(sh.url!),
            },
          });
        },
        onError: (e) => toast.error(message(e, t`Could not create link`)),
      },
    );
  }

  function revoke(s: Share) {
    revokeShare.mutate(
      { nodeId: node.id, shareId: s.id },
      {
        onSuccess: () => toast.success(t`Access revoked`),
        onError: (e) => toast.error(message(e, t`Could not revoke`)),
      },
    );
  }

  const capsLabel = (c: GrantCaps) =>
    [
      c.level === "edit" ? t`edit` : t`view`,
      c.can_download && t`download`,
      c.can_share && t`re-share`,
    ]
      .filter(Boolean)
      .join(" · ");

  // The revoke confirmation is one sentence with three holes in it, so the
  // pieces are named here: a translator sees `{revokeWho}`, a select on the
  // node kind and a plural on the count, not three spliced fragments.
  const revokeWho = pendingRevoke?.grantee_email ?? t`This person`;
  const revokeOwned = pendingRevoke?.grantee_owned ?? 0;
  const nodeKind = node.kind;

  return (
    <Dialog open={live} onOpenChange={(open) => !open && modal.resolve(null)}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            <Trans>Share “{nodeName}”</Trans>
          </DialogTitle>
          <DialogDescription>
            {node.team_folder ? (
              <Trans>
                Everyone listed here reaches this team folder and everything inside it. Nobody
                reaches it otherwise, including space admins.
              </Trans>
            ) : node.kind === "dir" ? (
              <Trans>Access given here also opens everything inside this folder.</Trans>
            ) : (
              <Trans>Give a group or a colleague access to this file.</Trans>
            )}
          </DialogDescription>
        </DialogHeader>

        <section className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
          <div>
            <h3 className="text-sm font-semibold">
              <Trans>Give access</Trans>
            </h3>
            <p className="text-muted-foreground mt-1 text-xs">
              <Trans>
                Groups and people come from this space. The access below applies to everyone picked.
              </Trans>
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
                    aria-label={t`Remove ${{ name: g.label }}`}
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
                <Trans>Access level</Trans>
              </Label>
              <ToggleGroup
                aria-labelledby="share-level-label"
                size="sm"
                variant="outline"
                value={[level]}
                onValueChange={(val) => val.length > 0 && setLevel(val[0] as "view" | "edit")}
              >
                <ToggleGroupItem value="view">
                  <Trans>Can view</Trans>
                </ToggleGroupItem>
                <ToggleGroupItem value="edit">
                  <Trans>Can edit</Trans>
                </ToggleGroupItem>
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
                  <Trans>Allow download</Trans>
                  {level === "edit" && (
                    <span className="text-muted-foreground ml-1">
                      <Trans>(included with edit)</Trans>
                    </span>
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
                  <Trans>Allow re-sharing</Trans>
                </Label>
              </div>
            </div>
            <Button
              className="ml-auto"
              size="sm"
              disabled={picked.length === 0 || busy}
              onClick={() => void submit()}
            >
              <Trans>Share</Trans>
            </Button>
          </div>
        </section>

        <Separator />

        <section className="grid gap-2">
          <div>
            <h3 className="text-sm font-semibold">
              <Trans>Current access</Trans>
            </h3>
            <p className="text-muted-foreground mt-1 text-xs">
              <Trans>
                Nobody else reaches this — not even a space admin — unless a line here says so.
              </Trans>
            </p>
          </div>

          <div className="bg-background rounded-md border">
            {grants.isPending || shares.isPending ? (
              <p className="text-muted-foreground p-3 text-sm">
                <Trans>Loading access…</Trans>
              </p>
            ) : grantRows.length + directShares.length + linkShares.length === 0 ? (
              <p className="text-muted-foreground p-3 text-sm">
                <Trans>
                  No one else has access yet — only you, and anyone who can reach a folder above
                  this.
                </Trans>
              </p>
            ) : (
              <ul className="divide-y">
                {grantRows.length > 0 && !inTeam && (
                  <li className="text-muted-foreground bg-muted/20 px-3 py-2 text-xs">
                    <Trans>
                      Revoking a group’s access leaves anything its members created in here — those
                      items stay with whoever made them.
                    </Trans>
                  </li>
                )}
                {grantRows.map((g) => (
                  <li key={g.id} className="flex items-center gap-3 p-3 text-sm">
                    <Users className="text-muted-foreground size-4 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-2 truncate font-medium">
                        {g.group_name}
                        {g.is_everyone && (
                          <Badge variant="secondary" className="gap-1 font-normal">
                            <Lock className="h-3 w-3" />
                            <Trans>Everyone</Trans>
                          </Badge>
                        )}
                        {g.is_admin && (
                          <Badge variant="secondary" className="gap-1 font-normal">
                            <ShieldCheck className="h-3 w-3" />
                            <Trans>Admins</Trans>
                          </Badge>
                        )}
                      </p>
                      <p className="text-muted-foreground text-[10px] uppercase">
                        <Trans>Group</Trans>
                      </p>
                    </div>
                    <span className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">
                      {capsLabel(g)}
                    </span>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="shrink-0"
                      aria-label={t`Remove ${{ name: g.group_name }}'s access`}
                      onClick={() =>
                        revokeGrant.mutate(g.group_id, {
                          onSuccess: () =>
                            toast.success(t`${{ name: g.group_name }} no longer has access`),
                          onError: (e) => toast.error(message(e, t`Could not revoke`)),
                        })
                      }
                    >
                      <X />
                    </Button>
                  </li>
                ))}
                {[...directShares, ...linkShares].map((s) => {
                  // Only a direct share carries a count: the server cannot say
                  // which member of a group authored what. Zero inside a team
                  // folder, which also routes the revoke button straight past
                  // the confirmation — there is nothing left behind to warn
                  // about.
                  const owned = !inTeam && s.kind === "direct" ? (s.grantee_owned ?? 0) : 0;
                  const grantee = s.grantee_email ?? t`the public link`;
                  return (
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
                          {s.kind === "link" ? (
                            <Trans>Anyone with the link</Trans>
                          ) : (
                            s.grantee_email
                          )}
                        </p>
                        <p className="text-muted-foreground text-[10px] uppercase">
                          {s.kind === "link" ? (
                            <Trans>Public link</Trans>
                          ) : (
                            <Trans>Direct share</Trans>
                          )}
                        </p>
                        {owned > 0 && (
                          <p className="text-muted-foreground mt-0.5 text-xs">
                            <Trans>
                              Created <Plural value={owned} one="# item" other="# items" /> in here
                              — those stay theirs.
                            </Trans>
                          </p>
                        )}
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
                          <Copy className="size-3.5" /> <Trans>Open</Trans>
                        </a>
                      )}
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="shrink-0"
                        aria-label={t`Revoke access for ${grantee}`}
                        onClick={() => (owned > 0 ? setPendingRevoke(s) : revoke(s))}
                      >
                        <X />
                      </Button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </section>

        <Separator />

        <section className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
          <div>
            <h3 className="text-sm font-semibold">
              <Trans>Public link</Trans>
            </h3>
            <p className="text-muted-foreground mt-1 text-xs">
              <Trans>
                Anyone holding the link can view this item without signing in. A link never carries
                editing, and nobody can re-share it onward — download is the one choice it offers.
              </Trans>
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="link-download-toggle" className="text-xs">
                <Trans>Access</Trans>
              </Label>
              <div className="flex items-center gap-2 pt-1.5">
                <Checkbox
                  id="link-download-toggle"
                  checked={linkCanDownload}
                  onCheckedChange={(v) => setLinkCanDownload(Boolean(v))}
                />
                <Label htmlFor="link-download-toggle" className="text-xs font-normal">
                  <Trans>Allow download</Trans>
                </Label>
              </div>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="link-expiry" className="text-xs">
                <Trans>Expiration</Trans>
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
                <Trans>Seconds; use 0 for no expiration.</Trans>
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
            <Trans>Create public link</Trans>
          </Button>
        </section>

        {/* Inline rather than a nested better-modal: this confirmation belongs
            to one row of a list that is already open, and a second modal in
            the container would outlive the dialog it interrupts. */}
        <AlertDialog
          open={pendingRevoke !== null}
          onOpenChange={(open) => !open && setPendingRevoke(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                <Trans>Remove access</Trans>
              </AlertDialogTitle>
              <AlertDialogDescription>
                <Trans>
                  {revokeWho} loses access to this{" "}
                  <Select value={nodeKind} _dir="folder" other="file" />, but keeps the{" "}
                  <Plural value={revokeOwned} one="# item" other="# items" /> they created inside it
                  — ownership is theirs, so those appear at their own top level.
                </Trans>
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>
                <Trans>Cancel</Trans>
              </AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={(e) => {
                  e.preventDefault();
                  if (pendingRevoke) revoke(pendingRevoke);
                  setPendingRevoke(null);
                }}
              >
                <Trans>Remove access</Trans>
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  );
});
