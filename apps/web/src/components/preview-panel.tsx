import { Download, Eye, History, Link2, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Button } from "@workspace/ui/components/button";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { cn } from "@workspace/ui/lib/utils";
import {
  canPreview,
  capsOf,
  isOfficeFile,
  openEditor,
  hasThumbnail,
  previewUrl,
  thumbnailFit,
  type Node,
} from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { useShares, useVersions } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { NodeIcon } from "@/components/node-icon";
import { PreviewModal } from "@/components/preview-modal";
import { ShareDialog } from "@/components/share-dialog";

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
 * Right-hand details panel. It follows the selection rather than opening on
 * its own: one node gets the full sheet, several get a count, none gets a
 * prompt. The panel's own visibility is the toolbar's (i) toggle.
 */
export function DetailsPanel({ nodes, onClose }: { nodes: Node[]; onClose: () => void }) {
  const { t } = useLingui();
  const single = nodes.length === 1 ? nodes[0]! : null;
  const folders = nodes.filter((n) => n.kind === "dir").length;
  const files = nodes.length - folders;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <span className="flex-1 truncate text-xs font-medium" title={single?.name}>
          {single ? single.name : t`Details`}
        </span>
        <Button variant="ghost" size="icon-sm" aria-label={t`Close details`} onClick={onClose}>
          <X />
        </Button>
      </div>

      {single ? (
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
  const versions = useVersions(isFile ? node.id : null);
  const ownerName = node.owner;

  const hasThumb = hasThumbnail(node);

  return (
    <div className="flex flex-col gap-4 p-3">
      {/* Thumbnail */}
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

      {/* Primary actions. Each is hidden when the caller does not hold it:
          the panel describes one node, and on a shared folder the answer
          differs row by row. */}
      <div className="border-border flex items-stretch gap-1 border-y py-1">
        <PanelAction
          icon={Eye}
          label={isOfficeFile(node.name) ? t`Open` : t`Preview`}
          disabled={!isFile || (!canPreview(node) && !isOfficeFile(node.name))}
          onClick={() => {
            if (isOfficeFile(node.name)) openEditor(node);
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
          {shares.isPending ? (
            <p className="text-muted-foreground text-[11px]">{t`Loading…`}</p>
          ) : shares.data && shares.data.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              {shares.data.map((share) => (
                <div key={share.id} className="flex items-center gap-2">
                  <Avatar size="sm" className="shrink-0">
                    <AvatarFallback>
                      {/* eslint-disable-next-line lingui/no-unlocalized-strings -- avatar initial for a link share */}
                      {(share.grantee_email ?? "L").charAt(0).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <span className="min-w-0 flex-1 truncate text-xs">
                    {share.grantee_email ?? t`Anyone with the link`}
                  </span>
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
