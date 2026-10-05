import { Fragment } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { Plural, Select, Trans, useLingui } from "@lingui/react/macro";
import { msg, plural } from "@lingui/core/macro";
import { toast } from "sonner";
import { ChevronDown, Copy, Folder, Link2, Lock, Mail, ShieldCheck, Users, X } from "lucide-react";
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
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import {
  // Aliased: the Lingui `Select` macro above owns the bare name.
  Select as SelectRoot,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { Separator } from "@workspace/ui/components/separator";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import {
  ApiError,
  capsOf,
  covers,
  type GrantCaps,
  type InheritedAccess,
  type Node,
  type NodeCaps,
  type Share,
} from "@/lib/api";
import {
  useCreateShare,
  useGrants,
  useGroups,
  useInheritedAccess,
  useMe,
  usePendingShares,
  useRevokeGrant,
  useRevokePendingShare,
  useRevokeShare,
  useShares,
  useSpaceMembers,
  useSpaces,
  useUpsertGrant,
} from "@/lib/queries";
import { useKeyedState } from "@/lib/use-keyed-state";
import { GranteePicker, type Grantee } from "@/components/grantee-picker";
import { LoadingStatus } from "@/components/loading";

type Props = { node: Node };

/**
 * The server's problem+json message when there is one, else whatever the
 * failure carried. `fallback` arrives already translated: only the caller
 * knows which act failed.
 */
const message = (e: unknown, fallback: string) =>
  e instanceof ApiError ? e.message : ((e as Error)?.message ?? fallback);

/** One day in the seconds `expires_in` counts. */
const DAY = 86_400;

/**
 * The lifetimes a public link is offered, as `expires_in` seconds — "0" is
 * the server's "never". Presets rather than a number field: nobody thinks of
 * a deadline in seconds. Descriptors, because this is module scope and a
 * rendered label would freeze the first catalog loaded.
 */
const EXPIRY_PRESETS = [
  { value: String(DAY), label: msg`1 day` },
  { value: String(7 * DAY), label: msg`7 days` },
  { value: String(30 * DAY), label: msg`30 days` },
  { value: "0", label: msg`Never` },
];

/**
 * A week unless someone picks otherwise: a link is the one grant that works
 * for whoever holds it, so the default is one that ends.
 */
const DEFAULT_EXPIRY = String(7 * DAY);

/**
 * The shapes a grant's level and download flag can take together. Edit
 * implies download, so "edit without download" is not one of them; re-sharing
 * is independent of both and stays its own switch.
 */
type Preset = "view" | "download" | "edit";

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
 * The whole answer also includes everyone reaching the node from above it —
 * its owners and the grants on folders over it. Those rows are listed too,
 * read-only: the grant lives on the folder, and changing it there changes it
 * for everything inside, which is not what editing one file's access means.
 *
 * What the caller may hand out is what they hold (`covers`). Options wider
 * than their own access are left out, and a grant wider than theirs shows no
 * controls — the server refuses both, and a control that can only fail is
 * worse than none.
 *
 * Grantees come from the space, never from a tenant-wide directory: a space is
 * a company, and the picker must not double as an address book for all of them.
 */
export const ShareDialog = createModal<Props, null>("share", ({ node, modal }) => {
  const live = modal.visible;
  const { t, i18n } = useLingui();
  // Named local so the placeholder reaches a translator as `{nodeName}`.
  const nodeName = node.name;
  const held = capsOf(node);
  const me = useMe();
  // Groups and members are member-only reads. Someone holding share on a
  // node in a space they do not belong to picks from neither, and may not
  // invite by email either: the space's switch is theirs to see only as a
  // member.
  const space = useSpaces().data?.find((s) => s.id === node.space_id);
  const isMember = space !== undefined;
  const allowEmail =
    (me.data?.external_sharing_enabled ?? false) && space?.external_sharing === true;
  // A personal space's automatic groups hold only its owner: nothing to offer.
  const { data: groups = [] } = useGroups(
    live && isMember && space.kind !== "personal" ? node.space_id : null,
  );
  const { data: members = [] } = useSpaceMembers(live && isMember ? node.space_id : null);
  const grants = useGrants(live ? node.id : null);
  const shares = useShares(live ? node.id : null);
  const inherited = useInheritedAccess(live ? node.id : null);
  const pending = usePendingShares(live ? node.id : null);
  const pendingShares = pending.data ?? [];

  const upsertGrant = useUpsertGrant(node.id);
  const revokeGrant = useRevokeGrant(node.id);
  const createShare = useCreateShare();
  const revokeShare = useRevokeShare();
  const revokePending = useRevokePendingShare();

  // Keyed to the node: better-modal keeps a closed modal mounted, so plain
  // useState would carry the previous item's picks into the next opening and
  // could hand access to something nobody chose.
  const [picked, setPicked] = useKeyedState<string, Grantee[]>(node.id, []);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- access-level enum from the API
  const [level, setLevel] = useKeyedState<string, "view" | "edit">(node.id, "view");
  const [canDownload, setCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [canShare, setCanShare] = useKeyedState<string, boolean>(node.id, false);
  const [linkCanDownload, setLinkCanDownload] = useKeyedState<string, boolean>(node.id, false);
  const [expires, setExpires] = useKeyedState<string, string>(node.id, DEFAULT_EXPIRY);
  // Keyed to this opening, not just the node: the server keeps no copy of
  // the link, and "shown once" has to mean closing the dialog puts it away
  // rather than leaving it on screen for the next time someone opens it.
  const [linkUrl, setLinkUrl] = useKeyedState<string, string | null>(`${node.id}:${live}`, null);
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

  // The node's own owners lead the list; everyone else reaching it from above
  // is grouped under the folder the access is held on, nearest first — the
  // order the server sends.
  const owners: InheritedAccess[] = [];
  const above = new Map<string, { viaName: string; rows: InheritedAccess[] }>();
  for (const r of inherited.data ?? []) {
    if (r.via_id === node.id) {
      owners.push(r);
      continue;
    }
    const folder = above.get(r.via_id);
    if (folder) folder.rows.push(r);
    else above.set(r.via_id, { viaName: r.via_name, rows: [r] });
  }

  const pickedIds = new Set(picked.map((g) => g.id));
  const grantedGroups = new Set(grantRows.map((g) => g.group_id));
  const sharedUsers = new Set(directShares.map((s) => s.grantee_user_id));

  const options: Grantee[] = [];
  // Groups worth one click: the quick row above the picker, so giving a whole
  // team access does not need a search first.
  const quickGroups: Extract<Grantee, { kind: "group" }>[] = [];
  for (const g of groups) {
    // A group already granted is changed on its row below. Picking it again
    // would overwrite that grant with whatever this form says, unannounced.
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
    // Sharing with yourself is refused by the server, and someone who already
    // holds a grant is changed on their row below — picking them again would
    // silently replace it. Both belong out of the list.
    if (m.user_id === me.data?.id || sharedUsers.has(m.user_id) || pickedIds.has(m.user_id)) {
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
  // and an editor may be denied it. Each part is clamped to what the caller
  // holds, so a choice made before their access narrowed cannot be sent.
  // eslint-disable-next-line lingui/no-unlocalized-strings -- access-level enum from the API
  const chosenLevel = held.edit ? level : "view";
  const caps: GrantCaps = {
    level: chosenLevel,
    can_download: held.download && (chosenLevel === "edit" || canDownload),
    can_share: held.share && canShare,
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
              // An address may belong to someone outside the space, who may
              // never re-share; the server refuses the attempt outright.
              body:
                g.kind === "email"
                  ? // eslint-disable-next-line lingui/no-unlocalized-strings -- share kind discriminator
                    { kind: "direct", email: g.id, ...caps, can_share: false }
                  : // eslint-disable-next-line lingui/no-unlocalized-strings -- share kind discriminator
                    { kind: "direct", user_id: g.id, ...caps },
            }),
      ),
    );
    const failed: string[] = [];
    let firstError = "";
    let granted = 0;
    let firstGranted = "";
    results.forEach((r, i) => {
      if (r.status === "rejected") {
        failed.push(picked[i].label);
        firstError ||= shareError(r.reason);
        return;
      }
      // 202: the address has no account yet, so what was made is an
      // invitation, not access.
      if (r.value && "mail_sent" in r.value) {
        const invitee = r.value.email;
        if (r.value.mail_sent) toast.success(t`Invitation sent to ${invitee}`);
        else
          toast.warning(
            t`Invited ${invitee}, but no email could be sent — share the link yourself.`,
          );
        return;
      }
      granted += 1;
      firstGranted ||= picked[i].label;
    });
    if (granted > 0) {
      // One whole sentence per level rather than a verb spliced onto a
      // subject: "can now view" does not survive being assembled elsewhere.
      const who =
        granted === 1
          ? firstGranted
          : t`${plural(granted, { one: "# grantee", other: "# grantees" })}`;
      toast.success(caps.level === "edit" ? t`${who} can now edit` : t`${who} can now view`);
    }
    if (failed.length > 0) {
      // Names and a server message, no phrasing of our own to translate.
      toast.error(`${failed.join(", ")}: ${firstError}`);
    }
    setPicked(failed.length > 0 ? picked.filter((g) => failed.includes(g.label)) : []);
  }

  /** The refusals an invitation can meet, in words the sharer can act on. */
  function shareError(e: unknown) {
    if (e instanceof ApiError && e.status === 429) {
      return t`Too many invitations today. Try again tomorrow.`;
    }
    if (e instanceof ApiError && e.status === 403) {
      return e.message || t`Sharing outside this space is not allowed here.`;
    }
    return message(e, t`Could not share`);
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
          can_download: held.download && linkCanDownload,
          can_share: false,
          expires_in: Number(expires),
        },
      },
      {
        // The token is stored hashed, so this response is the only time the
        // URL exists anywhere the caller can read it. It stays in the dialog
        // until they close it, rather than in a toast that times out.
        onSuccess: (sh) => setLinkUrl("url" in sh ? (sh.url ?? null) : null),
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

  // The name a person's grant is shown and confirmed under. The address is
  // the fallback, not the headline: two colleagues are told apart by name.
  function personName(s: Share) {
    return s.grantee_name || s.grantee_email || t`This person`;
  }

  const expiryItems = EXPIRY_PRESETS.map((p) => ({ value: p.value, label: i18n._(p.label) }));

  // The revoke confirmation is one sentence with three holes in it, so the
  // pieces are named here: a translator sees `{revokeWho}`, a select on the
  // node kind and a plural on the count, not three spliced fragments.
  const revokeWho = pendingRevoke ? personName(pendingRevoke) : t`This person`;
  const revokeOwned = pendingRevoke?.grantee_owned ?? 0;
  const nodeKind = node.kind;

  const loading = grants.isPending || shares.isPending || inherited.isPending;
  const rowCount =
    owners.length +
    above.size +
    grantRows.length +
    directShares.length +
    linkShares.length +
    pendingShares.length;

  return (
    <Dialog open={live} onOpenChange={(open) => !open && modal.resolve(null)}>
      {/* The popup itself does not scroll, only the body below the header, so
          the title and the close button stay put however long the list gets. */}
      <DialogContent className="flex max-h-[85svh] flex-col gap-0 p-0 sm:max-w-xl">
        <DialogHeader className="border-b p-4 pr-10">
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

        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto p-4">
          <section className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
            <div>
              <h3 className="text-sm font-semibold">
                <Trans>Give access</Trans>
              </h3>
              <p className="text-muted-foreground mt-1 text-xs">
                {allowEmail ? (
                  <Trans>
                    Groups and people come from this space; anyone else can be invited by email. The
                    access below applies to everyone picked.
                  </Trans>
                ) : (
                  <Trans>
                    Groups and people come from this space. The access below applies to everyone
                    picked.
                  </Trans>
                )}
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
                    {g.kind === "group" ? (
                      <Users className="h-3 w-3" />
                    ) : g.kind === "email" ? (
                      <Mail className="h-3 w-3" />
                    ) : null}
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

            <GranteePicker
              options={options}
              allowEmail={allowEmail}
              onPick={(g) => setPicked([...picked, g])}
            />

            <div className="flex flex-wrap items-end gap-4">
              {/* Only an editor may make editors, so without edit there is no
                level to choose: whatever is given is view. */}
              {held.edit && (
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
              )}
              {(held.download || held.share) && (
                <div className="grid gap-1.5">
                  <Label id="share-perms-label" className="text-xs">
                    <Trans>Permissions</Trans>
                  </Label>
                  <ToggleGroup
                    multiple
                    aria-labelledby="share-perms-label"
                    size="sm"
                    variant="outline"
                    value={[
                      ...(chosenLevel === "edit" || canDownload ? ["download"] : []),
                      ...(canShare ? ["reshare"] : []),
                    ]}
                    onValueChange={(val) => {
                      // Edit implies download, so that item is pinned on and
                      // disabled; its state is never read back from here.
                      // eslint-disable-next-line lingui/no-unlocalized-strings -- toggle item id
                      if (chosenLevel !== "edit") setCanDownload(val.includes("download"));
                      // eslint-disable-next-line lingui/no-unlocalized-strings -- toggle item id
                      setCanShare(val.includes("reshare"));
                    }}
                  >
                    {held.download && (
                      <ToggleGroupItem
                        value="download"
                        disabled={chosenLevel === "edit"}
                        title={chosenLevel === "edit" ? t`Included with edit` : undefined}
                      >
                        <Trans>Allow download</Trans>
                      </ToggleGroupItem>
                    )}
                    {held.share && (
                      <ToggleGroupItem value="reshare">
                        <Trans>Allow re-sharing</Trans>
                      </ToggleGroupItem>
                    )}
                  </ToggleGroup>
                </div>
              )}
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
                  Nobody reaches this — not even a space admin — unless a line here says so.
                </Trans>
              </p>
            </div>

            <div className="bg-background rounded-md border">
              {loading ? (
                <LoadingStatus>
                  <ul className="divide-y">
                    {[45, 60, 35].map((w) => (
                      <li key={w} className="flex items-center gap-3 p-3">
                        <Skeleton className="size-6 shrink-0 rounded-full" />
                        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                          <Skeleton className="h-3.5" style={{ width: `${w}%` }} />
                          <Skeleton className="h-2.5 w-16" />
                        </div>
                      </li>
                    ))}
                  </ul>
                </LoadingStatus>
              ) : rowCount === 0 && !inherited.isError ? (
                <p className="text-muted-foreground p-3 text-sm">
                  <Trans>Nobody has access to this yet.</Trans>
                </p>
              ) : (
                <ul className="divide-y">
                  {owners.map((r) => (
                    <InheritedRow key={`${r.kind}:${r.principal_id}`} row={r} summary={null} />
                  ))}
                  {grantRows.length > 0 && !inTeam && (
                    <li className="text-muted-foreground bg-muted/20 px-3 py-2 text-xs">
                      <Trans>
                        Revoking a group’s access leaves anything its members created in here —
                        those items stay with whoever made them.
                      </Trans>
                    </li>
                  )}
                  {grantRows.map((g) => {
                    const changeable = covers(held, g);
                    return (
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
                        {/* Changing a grant in place is the same upsert that
                          created it: the group is PUT again and the server
                          updates its row. */}
                        {changeable ? (
                          <CapsControl
                            value={g}
                            held={held}
                            summary={capsLabel(g)}
                            label={t`Change access for ${{ name: g.group_name }}`}
                            disabled={busy}
                            onChange={(next) =>
                              upsertGrant.mutate(
                                { groupId: g.group_id, caps: next },
                                {
                                  onSuccess: () =>
                                    toast.success(t`Changed access for ${{ name: g.group_name }}`),
                                  onError: (e) =>
                                    toast.error(message(e, t`Could not change access`)),
                                },
                              )
                            }
                          />
                        ) : (
                          <CapsSummary text={capsLabel(g)} />
                        )}
                        {changeable && (
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
                        )}
                      </li>
                    );
                  })}
                  {[...directShares, ...linkShares].map((s) => {
                    // Only a direct share carries a count: the server cannot say
                    // which member of a group authored what. Zero inside a team
                    // folder, which also routes the revoke button straight past
                    // the confirmation — there is nothing left behind to warn
                    // about.
                    const owned = !inTeam && s.kind === "direct" ? (s.grantee_owned ?? 0) : 0;
                    const grantee = s.kind === "direct" ? personName(s) : t`the public link`;
                    const changeable = covers(held, s);
                    // A person's grant is changed by posting it again under
                    // their id; a link has no such update, only revoke.
                    const userId = s.kind === "direct" ? s.grantee_user_id : undefined;
                    const email =
                      s.kind === "direct" && s.grantee_name && s.grantee_email !== s.grantee_name
                        ? s.grantee_email
                        : undefined;
                    return (
                      <li key={s.id} className="flex items-center gap-3 p-3 text-sm">
                        <Avatar size="sm">
                          {s.kind === "direct" ? (
                            <AvatarFallback>{grantee.charAt(0).toUpperCase()}</AvatarFallback>
                          ) : (
                            <AvatarFallback>
                              <Link2 className="size-3.5" />
                            </AvatarFallback>
                          )}
                        </Avatar>
                        <div className="min-w-0 flex-1">
                          <p className="flex items-center gap-2 truncate font-medium">
                            {s.kind === "link" ? <Trans>Anyone with the link</Trans> : grantee}
                            {s.is_external && (
                              <Badge variant="outline" className="font-normal">
                                <Trans>External</Trans>
                              </Badge>
                            )}
                          </p>
                          {s.kind === "link" ? (
                            <p className="text-muted-foreground text-[10px] uppercase">
                              <Trans>Public link</Trans>
                            </p>
                          ) : email ? (
                            <p className="text-muted-foreground truncate text-xs">{email}</p>
                          ) : (
                            <p className="text-muted-foreground text-[10px] uppercase">
                              <Trans>Direct share</Trans>
                            </p>
                          )}
                          {owned > 0 && (
                            <p className="text-muted-foreground mt-0.5 text-xs">
                              <Trans>
                                Created <Plural value={owned} one="# item" other="# items" /> in
                                here — those stay theirs.
                              </Trans>
                            </p>
                          )}
                        </div>
                        {changeable && userId ? (
                          <CapsControl
                            value={s}
                            held={held}
                            summary={capsLabel(s)}
                            label={t`Change access for ${{ name: grantee }}`}
                            disabled={busy}
                            noReshare={s.is_external === true}
                            onChange={(next) =>
                              createShare.mutate(
                                {
                                  nodeId: node.id,
                                  // eslint-disable-next-line lingui/no-unlocalized-strings -- share kind discriminator
                                  body: { kind: "direct", user_id: userId, ...next },
                                },
                                {
                                  onSuccess: () =>
                                    toast.success(t`Changed access for ${{ name: grantee }}`),
                                  onError: (e) =>
                                    toast.error(message(e, t`Could not change access`)),
                                },
                              )
                            }
                          />
                        ) : (
                          <CapsSummary text={capsLabel(s)} />
                        )}
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
                        {changeable && (
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            className="shrink-0"
                            aria-label={t`Revoke access for ${grantee}`}
                            onClick={() => (owned > 0 ? setPendingRevoke(s) : revoke(s))}
                          >
                            <X />
                          </Button>
                        )}
                      </li>
                    );
                  })}
                  {pendingShares.map((p) => {
                    const invitee = p.email;
                    return (
                      <li key={`pending:${p.id}`} className="flex items-center gap-3 p-3 text-sm">
                        <Avatar size="sm">
                          <AvatarFallback>
                            <Mail className="size-3.5" />
                          </AvatarFallback>
                        </Avatar>
                        <div className="min-w-0 flex-1">
                          <p className="flex items-center gap-2 truncate font-medium">
                            {invitee}
                            <Badge variant="outline" className="font-normal">
                              <Trans>Invited</Trans>
                            </Badge>
                          </p>
                          <p className="text-muted-foreground text-xs">
                            <Trans>Gets access after signing in for the first time.</Trans>
                          </p>
                        </div>
                        <CapsSummary text={capsLabel({ ...p, can_share: false })} />
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          className="shrink-0"
                          aria-label={t`Cancel the invitation for ${invitee}`}
                          disabled={revokePending.isPending}
                          onClick={() =>
                            revokePending.mutate(
                              { nodeId: node.id, pendingId: p.id },
                              {
                                onSuccess: () => toast.success(t`Invitation cancelled`),
                                onError: (e) => toast.error(message(e, t`Could not revoke`)),
                              },
                            )
                          }
                        >
                          <X />
                        </Button>
                      </li>
                    );
                  })}
                  {above.size > 0 && (
                    <li className="text-muted-foreground bg-muted/20 px-3 py-2 text-xs">
                      <Trans>
                        Access from the folders above. Change these on the folder they come from.
                      </Trans>
                    </li>
                  )}
                  {[...above].map(([viaId, { viaName, rows }]) => (
                    <Fragment key={viaId}>
                      <li className="text-muted-foreground flex items-center gap-1.5 px-3 pt-2.5 pb-1.5 text-xs font-medium">
                        <Folder className="size-3.5 shrink-0" />
                        <span className="truncate">
                          <Trans>From {viaName}</Trans>
                        </span>
                      </li>
                      {rows.map((r) => (
                        <InheritedRow
                          key={`${r.kind}:${r.principal_id}`}
                          row={r}
                          summary={r.kind === "owner" ? null : capsLabel(r)}
                        />
                      ))}
                    </Fragment>
                  ))}
                  {inherited.isError && (
                    <li className="text-destructive px-3 py-2 text-xs">
                      <Trans>Could not load the access that comes from folders above this.</Trans>
                    </li>
                  )}
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
                  Anyone holding the link can view this item without signing in. A link never
                  carries editing, and nobody can re-share it onward — download is the one choice it
                  offers.
                </Trans>
              </p>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {/* A link can only pass on what its creator holds: without
                download there is no choice left to offer. */}
              {held.download && (
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
              )}
              <div className="grid gap-1.5">
                <Label htmlFor="link-expiry" className="text-xs">
                  <Trans>Expiration</Trans>
                </Label>
                <SelectRoot
                  value={expires}
                  items={expiryItems}
                  onValueChange={(v) => setExpires((v as string) ?? DEFAULT_EXPIRY)}
                >
                  <SelectTrigger id="link-expiry" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {expiryItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </SelectRoot>
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
            {linkUrl && (
              <div className="grid gap-1.5">
                <div className="flex items-center gap-2">
                  <Input
                    readOnly
                    value={linkUrl}
                    aria-label={t`New public link`}
                    aria-describedby="link-once-help"
                    onFocus={(e) => e.currentTarget.select()}
                  />
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      void navigator.clipboard.writeText(linkUrl).then(
                        () => toast.success(t`Link copied`),
                        () => toast.error(t`Could not copy the link`),
                      )
                    }
                  >
                    <Copy />
                    <Trans>Copy</Trans>
                  </Button>
                </div>
                <p id="link-once-help" className="text-muted-foreground text-xs">
                  <Trans>
                    This link is shown only once. Copy it now — it cannot be shown again after this
                    dialog closes.
                  </Trans>
                </p>
              </div>
            )}
          </section>
        </div>

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

/** A grant's access as plain text, for a row the caller may not change. */
function CapsSummary({ text }: { text: string }) {
  return <span className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">{text}</span>;
}

/**
 * A grant's access, changeable in place.
 *
 * Only rendered on a row the caller covers, and offering only what they hold
 * — "Can edit" needs edit, download needs download — so every choice here is
 * one the server will accept. The current value is the server's: the menu
 * does not pretend a change landed before the refetch says so.
 */
function CapsControl({
  value,
  held,
  summary,
  label,
  disabled,
  onChange,
  noReshare = false,
}: {
  value: GrantCaps;
  held: NodeCaps;
  summary: string;
  label: string;
  disabled: boolean;
  onChange: (next: GrantCaps) => void;
  /** Outsiders may not re-share: the option is left out rather than refused. */
  noReshare?: boolean;
}) {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- preset ids
  const preset: Preset = value.level === "edit" ? "edit" : value.can_download ? "download" : "view";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={label}
        render={
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground shrink-0 font-normal whitespace-nowrap"
          />
        }
      >
        {summary}
        <ChevronDown data-icon="inline-end" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuRadioGroup
          value={preset}
          onValueChange={(next: Preset) => {
            if (next === preset) return;
            onChange({
              // eslint-disable-next-line lingui/no-unlocalized-strings -- access-level enum from the API
              level: next === "edit" ? "edit" : "view",
              can_download: next !== "view",
              can_share: value.can_share,
            });
          }}
        >
          <DropdownMenuRadioItem value="view">
            <Trans>Can view</Trans>
          </DropdownMenuRadioItem>
          {held.download && (
            <DropdownMenuRadioItem value="download">
              <Trans>Can view + download</Trans>
            </DropdownMenuRadioItem>
          )}
          {held.edit && (
            <DropdownMenuRadioItem value="edit">
              <Trans>Can edit</Trans>
            </DropdownMenuRadioItem>
          )}
        </DropdownMenuRadioGroup>
        {held.share && !noReshare && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem
              checked={value.can_share}
              // Spelled out, not spread: `value` is the whole Share or Grant
              // row, and every field of it would ride along into the request.
              onCheckedChange={(on) =>
                onChange({ level: value.level, can_download: value.can_download, can_share: on })
              }
            >
              <Trans>Allow re-sharing</Trans>
            </DropdownMenuCheckboxItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Someone reaching the node from above it — or its owner. Read-only: the
 * grant lives on the folder named over it, and only that folder's dialog can
 * change it.
 */
function InheritedRow({ row, summary }: { row: InheritedAccess; summary: string | null }) {
  return (
    <li className="flex items-center gap-3 p-3 text-sm">
      {row.kind === "group" ? (
        <Users className="text-muted-foreground size-4 shrink-0" />
      ) : (
        <Avatar size="sm">
          <AvatarFallback>
            {row.kind === "link" ? (
              <Link2 className="size-3.5" />
            ) : (
              (row.name || "?").charAt(0).toUpperCase()
            )}
          </AvatarFallback>
        </Avatar>
      )}
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium">
          {/* The server's link name is an English literal; the label is ours. */}
          {row.kind === "link" ? <Trans>Anyone with the link</Trans> : row.name}
        </p>
        <p className="text-muted-foreground text-[10px] uppercase">
          {row.kind === "owner" ? (
            <Trans>Owner</Trans>
          ) : row.kind === "group" ? (
            <Trans>Group</Trans>
          ) : row.kind === "link" ? (
            <Trans>Public link</Trans>
          ) : (
            <Trans>Direct share</Trans>
          )}
        </p>
      </div>
      {summary && <CapsSummary text={summary} />}
    </li>
  );
}
