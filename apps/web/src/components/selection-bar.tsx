import { Download, Link2, Pencil, Trash2, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import type { Node } from "@/lib/api";

interface SelectionBarProps {
  selected: Node[];
  onClear: () => void;
  onDownload: (node: Node) => void;
  onShare: (node: Node) => void;
  onRename: (node: Node) => void;
  onDelete: (nodes: Node[]) => void;
}

/**
 * Floating action bar for a multi-row selection.
 *
 * The Stitch design also offers Move and Copy; the API has no endpoint for
 * either, so they are omitted rather than rendered as controls that no-op.
 * Download, Share and Rename act on a single node, so they appear only when
 * the selection is exactly one; Delete works on the whole selection.
 */
export function SelectionBar({
  selected,
  onClear,
  onDownload,
  onShare,
  onRename,
  onDelete,
}: SelectionBarProps) {
  if (selected.length === 0) return null;

  const only = selected.length === 1 ? selected[0] : null;

  return (
    <div
      role="toolbar"
      aria-label="Selection actions"
      className="border-border bg-popover absolute bottom-6 left-1/2 z-30 flex h-10 -translate-x-1/2 items-center gap-2 rounded-lg border px-3 shadow-sm"
    >
      <span className="text-xs font-medium whitespace-nowrap">{selected.length} selected</span>
      <span className="bg-border h-4 w-px" />

      <div className="flex items-center gap-1">
        {only?.kind === "file" && (
          <Button variant="ghost" size="sm" onClick={() => onDownload(only)}>
            <Download data-icon="inline-start" /> Download
          </Button>
        )}
        {only && (
          <>
            <Button variant="ghost" size="sm" onClick={() => onShare(only)}>
              <Link2 data-icon="inline-start" /> Share
            </Button>
            <Button variant="ghost" size="sm" onClick={() => onRename(only)}>
              <Pencil data-icon="inline-start" /> Rename
            </Button>
          </>
        )}
      </div>

      <span className="bg-border h-4 w-px" />
      <Button variant="destructive" size="sm" onClick={() => onDelete(selected)}>
        <Trash2 data-icon="inline-start" /> Delete
      </Button>

      <Button variant="ghost" size="icon-sm" aria-label="Clear selection" onClick={onClear}>
        <X />
      </Button>
    </div>
  );
}
