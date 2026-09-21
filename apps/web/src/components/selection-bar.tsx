import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Download, FolderInput, Link2, Pencil, Trash2, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { canRestructure, capsOf, commonCaps, type Node } from "@/lib/api";

interface SelectionBarProps {
  selected: Node[];
  onClear: () => void;
  onDownload: (node: Node) => void;
  onShare: (node: Node) => void;
  onRename: (node: Node) => void;
  onMove: (nodes: Node[]) => void;
  onDelete: (nodes: Node[]) => void;
}

/**
 * Floating action bar for a multi-row selection.
 *
 * Download, Share and Rename act on a single node, so they appear only when
 * the selection is exactly one; Move and Delete work on the whole selection.
 *
 * Every control is gated on what the caller holds — for the bulk ones, on
 * what they hold across the *whole* selection. A selection can mix a file you
 * own with one you were only granted a look at, and "Delete 5 items" must not
 * mean "delete the two of them I'm allowed to".
 *
 * The Stitch design also offers Copy, which has no endpoint, so it is omitted
 * rather than rendered as a control that no-ops.
 */
export function SelectionBar({
  selected,
  onClear,
  onDownload,
  onShare,
  onRename,
  onMove,
  onDelete,
}: SelectionBarProps) {
  const { t } = useLingui();
  if (selected.length === 0) return null;

  const only = selected.length === 1 ? selected[0] : null;
  const onlyCaps = capsOf(only);
  const bulk = commonCaps(selected);
  const canDelete = selected.every(canRestructure);
  const canRename = only ? canRestructure(only) : false;
  return (
    <div
      role="toolbar"
      aria-label={t`Selection actions`}
      // The bar acts on the selection, so a click inside it must not count as
      // a click on the listing's background, which clears it.
      data-keeps-selection=""
      className="border-border bg-popover absolute bottom-6 left-1/2 z-30 flex h-10 -translate-x-1/2 items-center gap-2 rounded-lg border px-3 shadow-sm"
    >
      <span className="text-xs font-medium whitespace-nowrap">
        <Plural value={selected.length} one="# selected" other="# selected" />
      </span>
      <span className="bg-border h-4 w-px" />

      <div className="flex items-center gap-1">
        {only?.kind === "file" && onlyCaps.download && (
          <Button variant="ghost" size="sm" onClick={() => onDownload(only)}>
            <Download data-icon="inline-start" /> <Trans>Download</Trans>
          </Button>
        )}
        {only && onlyCaps.share && (
          <Button variant="ghost" size="sm" onClick={() => onShare(only)}>
            <Link2 data-icon="inline-start" /> <Trans>Share</Trans>
          </Button>
        )}
        {only && canRename && (
          <Button variant="ghost" size="sm" onClick={() => onRename(only)}>
            <Pencil data-icon="inline-start" /> <Trans>Rename</Trans>
          </Button>
        )}
        {bulk.edit && (
          <Button variant="ghost" size="sm" onClick={() => onMove(selected)}>
            <FolderInput data-icon="inline-start" /> <Trans>Move</Trans>
          </Button>
        )}
      </div>

      {canDelete && (
        <>
          <span className="bg-border h-4 w-px" />
          <Button variant="destructive" size="sm" onClick={() => onDelete(selected)}>
            <Trash2 data-icon="inline-start" /> <Trans>Delete</Trans>
          </Button>
        </>
      )}

      <Button variant="ghost" size="icon-sm" aria-label={t`Clear selection`} onClick={onClear}>
        <X />
      </Button>
    </div>
  );
}
