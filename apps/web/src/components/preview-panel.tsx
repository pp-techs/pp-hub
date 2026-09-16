import { Download, Eye, History, Link2, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { cn } from "@workspace/ui/lib/utils";
import {
  canPreview,
  fmtDate,
  fmtSize,
  isOfficeFile,
  openEditor,
  hasThumbnail,
  previewUrl,
  thumbnailFit,
  type Node,
} from "@/lib/api";
import { useShares, useVersions } from "@/lib/queries";
import { describeSelection } from "@/lib/selection";
import { useUiStore } from "@/lib/ui-store";
import { NodeIcon } from "@/components/node-icon";
import { PreviewModal } from "@/components/preview-modal";

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
  const single = nodes.length === 1 ? nodes[0]! : null;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <span className="flex-1 truncate text-xs font-medium" title={single?.name}>
          {single ? single.name : "Details"}
        </span>
        <Button variant="ghost" size="icon-sm" aria-label="Close details" onClick={onClose}>
          <X />
        </Button>
      </div>

      {single ? (
        <NodeDetails node={single} />
      ) : (
        <p className="text-muted-foreground p-3 text-xs">
          {nodes.length === 0
            ? "Select a file or folder to see its details."
            : `${describeSelection(nodes)} selected`}
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
  const openPanel = useUiStore((s) => s.openPanel);
  const shares = useShares(node.kind === "file" ? node.id : null);
  const versions = useVersions(node.kind === "file" ? node.id : null);

  const isFile = node.kind === "file";
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

      {/* Primary actions */}
      <div className="border-border flex items-stretch gap-1 border-y py-1">
        <PanelAction
          icon={Eye}
          label={isOfficeFile(node.name) ? "Open" : "Preview"}
          disabled={!isFile || (!canPreview(node) && !isOfficeFile(node.name))}
          onClick={() => {
            if (isOfficeFile(node.name)) openEditor(node);
            else void PreviewModal.show({ node });
          }}
        />
        <PanelAction
          icon={Download}
          label="Download"
          disabled={!isFile}
          onClick={() => window.open(`/api/v1/nodes/${node.id}/download`, "_blank")}
        />
        <PanelAction icon={Link2} label="Share" onClick={() => openPanel("share", node)} />
        <PanelAction
          icon={History}
          label="Versions"
          disabled={!isFile}
          onClick={() => openPanel("versions", node)}
        />
      </div>

      {/* Metadata */}
      <div>
        <SectionLabel>Details</SectionLabel>
        <div className="divide-border divide-y">
          <DetailRow label="Type" value={isFile ? (node.mime ?? "File") : "Folder"} />
          {isFile && (
            <DetailRow label="Size" value={node.size != null ? fmtSize(node.size) : "—"} />
          )}
          <DetailRow label="Owner" value={node.owner ?? "—"} />
          <DetailRow label="Modified" value={node.modified_at ? fmtDate(node.modified_at) : "—"} />
          {isFile && versions.data && <DetailRow label="Versions" value={versions.data.length} />}
        </div>
      </div>

      {/* Sharing */}
      {isFile && (
        <div>
          <SectionLabel>Sharing</SectionLabel>
          {shares.isPending ? (
            <p className="text-muted-foreground text-[11px]">Loading…</p>
          ) : shares.data && shares.data.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              {shares.data.map((share) => (
                <div key={share.id} className="flex items-center gap-2">
                  <Avatar size="sm" className="shrink-0">
                    <AvatarFallback>
                      {(share.grantee_email ?? "L").charAt(0).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <span className="min-w-0 flex-1 truncate text-xs">
                    {share.grantee_email ?? "Anyone with the link"}
                  </span>
                  <span className="text-muted-foreground shrink-0 text-[10px] uppercase">
                    {share.perm}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground text-[11px]">Not shared.</p>
          )}
        </div>
      )}
    </div>
  );
}
