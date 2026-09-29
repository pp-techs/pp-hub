import { Building2, Download, Eye, History, Link2, Settings2, Users, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Button } from "@workspace/ui/components/button";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import {
  canPreview,
  capsOf,
  openEditor,
  isMarkdownFile,
  opensInEditor,
  hasThumbnail,
  previewUrl,
  thumbnailFit,
  type Node,
  type Space,
} from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { useGrants, useShares, useSpace, useSpaceMembers, useVersions } from "@/lib/queries";
import { useSettingsNavigation } from "@/lib/use-settings-hash";
import { useUiStore } from "@/lib/ui-store";
import { LoadingStatus } from "@/components/loading";
import { NodeIcon } from "@/components/node-icon";
import { NodeMarkdownView } from "@/components/markdown-document-view";
import { PreviewModal } from "@/components/preview-modal";
import { ShareDialog } from "@/components/share-dialog";
import { storagePct } from "@/components/storage-meter";

/** Uppercase section heading inside the panel. */
function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-muted-foreground mb-2 text-[11px] font-medium tracking-wider uppercase">
      {children}
    </div>
  );
}

/** One label/value line in the details list. */
function DetailRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1">
      <span className="text-muted-foreground shrink-0 text-[11px]">{label}</span>
      <span className="min-w-0 truncate text-right text-xs">{value}</span>
    </div>
  );
}

function PanelAction({
  icon: Icon,
  label,
  onClick,
  disabled,
}: {
  icon: LucideIcon;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <Button
      variant="ghost"
      onClick={onClick}
      disabled={disabled}
      className="h-auto flex-1 flex-col gap-1 py-2"
    >
      <Icon className="size-4" />
      <span className="text-[10px] font-normal">{label}</span>
    </Button>
  );
}

/**
 * What the panel is describing. A space root has no node row of its own —
 * top-level nodes are `parent_id IS NULL`, a set rather than a child of
 * anything — so it is a subject in its own right rather than a fabricated
 * folder. `nodes: []` is the third, momentary state: the folder named by
 * `?dir=` has not been fetched yet.
 */
export type DetailsSubject =
  | { kind: "nodes"; nodes: Node[] }
  | { kind: "space"; spaceId: string; folders: number | null; files: number | null };

/**
 * Right-hand details panel. It follows the selection rather than opening on
 * its own: one node gets the full sheet, several get a count, and with none
 * the surface itself is the subject — the folder being browsed, or the space
 * at its root. The panel's own visibility is the toolbar's (i) toggle.
 */
export function DetailsPanel({
  subject,
  onClose,
}: {
  subject: DetailsSubject;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const space = useSpace(subject.kind === "space" ? subject.spaceId : undefined);
  const nodes = subject.kind === "nodes" ? subject.nodes : [];
  const single = nodes.length === 1 ? nodes[0]! : null;
  const folders = nodes.filter((n) => n.kind === "dir").length;
  const files = nodes.length - folders;
  const title = subject.kind === "space" ? space?.name : single?.name;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <span className="flex-1 truncate text-xs font-medium" title={title}>
          {title ?? t`Details`}
        </span>
        <Button variant="ghost" size="icon-sm" aria-label={t`Close details`} onClick={onClose}>
          <X />
        </Button>
      </div>

      {subject.kind === "space" ? (
        // Nothing rather than a sheet of em-dashes: the spaces query answers
        // in one round trip and every row here comes from it.
        space && <SpaceDetails space={space} folders={subject.folders} files={subject.files} />
      ) : single ? (
        <NodeDetails node={single} />
      ) : (
        <p className="text-muted-foreground p-3 text-xs">
          {/* A mixed selection stays one message, with both counts nested
              inside it: the conjunction and the clause order are the
              translator's to move, and a sentence joined from two halves
              would not let them. */}
          {nodes.length === 0 ? (
            <Trans>Select a file or folder to see its details.</Trans>
          ) : folders === 0 ? (
            <Plural value={files} one="# file selected" other="# files selected" />
          ) : files === 0 ? (
            <Plural value={folders} one="# folder selected" other="# folders selected" />
          ) : (
            <Trans>
              <Plural value={folders} one="# folder" other="# folders" /> and{" "}
              <Plural value={files} one="# file" other="# files" /> selected
            </Trans>
          )}
        </p>
      )}
    </div>
  );
}

/**
 * The space sheet, shown at the root of a space where there is no folder to
 * describe. Every figure is already in cache from the listing except the
 * member count, whose endpoint is member-readable and only queried while this
 * is mounted.
 *
 * `folders`/`files` count what the listing is showing — the Archive is
 * excluded and an active filter narrows them — because the panel sits beside
 * those rows and should not contradict them. `null` while the listing has no
 * answer yet: "0 folders and 0 files" would be a claim, not a placeholder.
 */
function SpaceDetails({
  space,
  folders,
  files,
}: {
  space: Space;
  folders: number | null;
  files: number | null;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  const { openSettings } = useSettingsNavigation();
  const members = useSpaceMembers(space.id);

  const pct = storagePct(space);
  const used = f.size(space.used_bytes);
  const quota = f.size(space.quota_bytes);

  return (
    <div className="flex flex-col gap-4 p-3">
      <div className="flex items-center gap-3">
        <div className="bg-muted/50 flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-md">
          {space.avatar_url ? (
            <img src={space.avatar_url} alt="" className="size-10 object-cover" />
          ) : (
            <Building2 className="text-muted-foreground size-5" />
          )}
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{space.name}</p>
          {space.description && (
            <p className="text-muted-foreground line-clamp-2 text-[11px]">{space.description}</p>
          )}
        </div>
      </div>

      {/* Both panes are admin-only — the settings dialog bounces a member to
          the account pane — so a member is offered neither. */}
      {space.is_admin && (
        <div className="border-border flex items-stretch gap-1 border-y py-1">
          <PanelAction
            icon={Settings2}
            label={t`Space settings`}
            // eslint-disable-next-line lingui/no-unlocalized-strings -- settings pane slug
            onClick={() => openSettings("space")}
          />
          <PanelAction
            icon={Users}
            label={t`People`}
            // eslint-disable-next-line lingui/no-unlocalized-strings -- settings pane slug
            onClick={() => openSettings("members")}
          />
        </div>
      )}

      <div>
        <SectionLabel>
          <Trans>Storage</Trans>
        </SectionLabel>
        {pct !== null && (
          <div className="bg-muted mb-1.5 h-1 w-full overflow-hidden rounded-full">
            <div className="bg-primary h-full rounded-full" style={{ width: `${pct}%` }} />
          </div>
        )}
        {/* One message per phrasing, not "used" glued onto a formatted size:
            the two halves swap order in plenty of languages. */}
        <p className="text-muted-foreground text-[11px] tabular-nums">
          {pct !== null ? (
            <Trans>
              {used} of {quota}
            </Trans>
          ) : (
            <Trans>{used} used</Trans>
          )}
        </p>
      </div>

      <div>
        <SectionLabel>
          <Trans>Details</Trans>
        </SectionLabel>
        <div className="divide-border divide-y">
          <DetailRow
            label={t`Contents`}
            value={
              folders === null || files === null ? (
                "—"
              ) : (
                <Trans>
                  <Plural value={folders} one="# folder" other="# folders" /> and{" "}
                  <Plural value={files} one="# file" other="# files" />
                </Trans>
              )
            }
          />
          <DetailRow label={t`Members`} value={members.data ? members.data.length : "—"} />
          <DetailRow label={t`Your access`} value={space.is_admin ? t`Admin` : t`Member`} />
        </div>
      </div>
    </div>
  );
}

/**
 * The single-node sheet: a thumbnail, the actions you reach for most, then the
 * metadata and sharing state.
 *
 * The Stitch design closes with an activity timeline; the API exposes no
 * activity feed, so that section is left out rather than filled with
 * placeholder events.
 */
function NodeDetails({ node }: { node: Node }) {
  const { t } = useLingui();
  const f = useFormatters();
  const openPanel = useUiStore((s) => s.openPanel);
  const caps = capsOf(node);
  const isFile = node.kind === "file";
  // Who else holds this is a sharer's question, and the endpoint answers 403
  // to anyone else — so the query is not made, rather than made and mourned.
  const shares = useShares(isFile && caps.share ? node.id : null);
  // Group rows of /shares carry no name to show; /grants is the labelled view
  // of the same rows, read the same way the share dialog reads it.
  const grants = useGrants(isFile && caps.share ? node.id : null);
  const access = [
    ...(grants.data ?? []).map((g) => ({ ...g, kind: "group" as const, label: g.group_name })),
    ...(shares.data ?? [])
      .filter((s) => s.kind !== "group")
      .map((s) => ({
        ...s,
        label:
          s.kind === "link" ? t`Anyone with the link` : s.grantee_name || s.grantee_email || "",
      })),
  ];
  const versions = useVersions(isFile ? node.id : null);
  const ownerName = node.owner;

  const hasThumb = hasThumbnail(node);
  // A markdown document has no thumbnail artifact; its first screen,
  // rendered, says more than an icon. Same capability as reading it.
  const showsDocument = caps.view && isMarkdownFile(node);

  return (
    <div className="flex flex-col gap-4 p-3">
      {/* Thumbnail */}
      {showsDocument ? (
        <div className="bg-background border-border relative aspect-[4/3] overflow-hidden rounded-md border">
          {/* Half scale: a page's opening lines at a glance. `inert` keeps
              its links and toggles out of the tab order of a sheet whose
              actions are below. */}
          <div inert className="pointer-events-none w-[200%] origin-top-left scale-50 select-none">
            <NodeMarkdownView node={node} className="p-6" />
          </div>
          <div className="from-background pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t" />
        </div>
      ) : (
        <div className="bg-muted/50 flex aspect-[4/3] items-center justify-center overflow-hidden rounded-md">
          {hasThumb ? (
            <img
              src={previewUrl(node)}
              alt=""
              className={cn(
                "size-full",
                thumbnailFit(node) === "cover" ? "object-cover" : "object-contain",
              )}
            />
          ) : (
            <NodeIcon node={node} className="size-10" />
          )}
        </div>
      )}

      {/* Primary actions. Each is hidden when the caller does not hold it:
          the panel describes one node, and on a shared folder the answer
          differs row by row. */}
      <div className="border-border flex items-stretch gap-1 border-y py-1">
        <PanelAction
          icon={Eye}
          label={opensInEditor(node) ? t`Open` : t`Preview`}
          disabled={!isFile || (!canPreview(node) && !opensInEditor(node))}
          onClick={() => {
            if (opensInEditor(node)) openEditor(node);
            else void PreviewModal.show({ node });
          }}
        />
        {caps.download && (
          <PanelAction
            icon={Download}
            label={t`Download`}
            disabled={!isFile}
            // eslint-disable-next-line lingui/no-unlocalized-strings -- API route path and window.open target
            onClick={() => window.open(`/api/v1/nodes/${node.id}/download`, "_blank")}
          />
        )}
        {caps.share && (
          <PanelAction
            icon={Link2}
            label={t`Share`}
            onClick={() => void ShareDialog.show({ node })}
          />
        )}
        <PanelAction
          icon={History}
          label={t`Versions`}
          disabled={!isFile}
          // eslint-disable-next-line lingui/no-unlocalized-strings -- panel id
          onClick={() => openPanel("versions", node)}
        />
      </div>

      {/* Metadata */}
      <div>
        <SectionLabel>
          <Trans>Details</Trans>
        </SectionLabel>
        <div className="divide-border divide-y">
          <DetailRow label={t`Type`} value={isFile ? (node.mime ?? t`File`) : t`Folder`} />
          {isFile && (
            <DetailRow label={t`Size`} value={node.size != null ? f.size(node.size) : "—"} />
          )}
          {/* A team folder and its contents have no personal owner: the
              creator is recorded, but access follows the folder's grants and
              survives them leaving. Saying "Owner: Alice" would be a lie about
              who controls it, so the holder is named on the labelled line and
              the person is demoted to a byline under it. The wrapper keeps
              both on one row of the divided list. */}
          {node.in_team ? (
            <div>
              <DetailRow label={t`Managed by`} value={t`Team folder`} />
              {node.owner && (
                <p className="text-muted-foreground pb-1 text-right text-[11px]">
                  <Trans>Added by {ownerName}</Trans>
                </p>
              )}
            </div>
          ) : (
            <DetailRow label={t`Owner`} value={node.owner ?? "—"} />
          )}
          <DetailRow
            label={t`Modified`}
            value={node.modified_at ? f.date(node.modified_at) : "—"}
          />
          {isFile && versions.data && (
            <DetailRow label={t`Versions`} value={versions.data.length} />
          )}
        </div>
      </div>

      {/* Sharing */}
      {isFile && caps.share && (
        <div>
          <SectionLabel>
            <Trans>Sharing</Trans>
          </SectionLabel>
          {shares.isPending || grants.isPending ? (
            <LoadingStatus className="flex flex-col gap-1.5">
              {[60, 45].map((w) => (
                <div key={w} className="flex items-center gap-2">
                  <Skeleton className="size-6 shrink-0 rounded-full" />
                  <Skeleton className="h-3" style={{ width: `${w}%` }} />
                  <Skeleton className="ml-auto h-2.5 w-8" />
                </div>
              ))}
            </LoadingStatus>
          ) : access.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              {access.map((share) => (
                <div key={share.id} className="flex items-center gap-2">
                  <Avatar size="sm" className="shrink-0">
                    <AvatarFallback>
                      {share.kind === "group" ? (
                        <Users className="size-3" />
                      ) : share.kind === "link" ? (
                        <Link2 className="size-3" />
                      ) : (
                        share.label.charAt(0).toUpperCase()
                      )}
                    </AvatarFallback>
                  </Avatar>
                  <span className="min-w-0 flex-1 truncate text-xs">{share.label}</span>
                  <span className="text-muted-foreground shrink-0 text-[10px] uppercase">
                    {share.level === "edit" ? t`edit` : t`view`}
                  </span>
                  <span className="flex items-center gap-0.5 text-muted-foreground text-[10px]">
                    {share.can_download && <span title={t`Can download`}>↓</span>}
                    {share.can_share && <span title={t`Can share`}>↗</span>}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground text-[11px]">
              <Trans>Not shared.</Trans>
            </p>
          )}
        </div>
      )}
    </div>
  );
}
