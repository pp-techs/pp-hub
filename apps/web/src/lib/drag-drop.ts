import type { Node } from "@/lib/api";

export interface DraggingNodes {
  spaceId: string;
  nodes: Node[];
}

// eslint-disable-next-line lingui/no-unlocalized-strings -- MIME data transfer type
export const DRAG_DATA_TYPE = "application/x-pphub-nodes";

/**
 * Walks upward through `dirs` to check if `targetId` is inside `ancestorId`'s subtree,
 * or is `ancestorId` itself. Cycles terminate safely via a `seen` Set.
 */
export function isDescendantOf(
  targetId: string,
  ancestorId: string,
  dirs: Record<string, { parent?: string }>,
): boolean {
  if (targetId === ancestorId) return true;
  const seen = new Set<string>();
  let cur: string | undefined = targetId;
  while (cur && !seen.has(cur)) {
    if (cur === ancestorId) return true;
    seen.add(cur);
    cur = dirs[cur]?.parent;
  }
  return false;
}

/**
 * Returns true if the active `dragging` state can be dropped into `targetId`.
 *
 * Requirements:
 * - Active drag exists and belongs to the same space.
 * - Target is not identical to any dragged node.
 * - At least one dragged node has a parent different from targetId.
 * - Target is not a descendant of any dragged directory.
 */
export function isValidMoveTarget(
  targetId: string | undefined,
  dragging: DraggingNodes | null,
  currentSpaceId: string,
  dirs: Record<string, { parent?: string }>,
): boolean {
  if (!dragging || dragging.nodes.length === 0) return false;
  if (dragging.spaceId !== currentSpaceId) return false;

  // Cannot drop into any of the dragged nodes themselves
  if (targetId !== undefined && dragging.nodes.some((n) => n.id === targetId)) {
    return false;
  }

  // At least one dragged node must not already reside in targetId
  const hasNodeMovingToNewParent = dragging.nodes.some(
    (n) => (n.parent_id ?? undefined) !== (targetId ?? undefined),
  );
  if (!hasNodeMovingToNewParent) return false;

  // If targetId is specified, it cannot be a descendant of any dragged directory
  if (targetId !== undefined) {
    for (const node of dragging.nodes) {
      if (node.kind === "dir" && isDescendantOf(targetId, node.id, dirs)) {
        return false;
      }
    }
  }

  return true;
}

/**
 * Creates an ephemeral, offscreen styled badge and attaches it as the HTML5 drag ghost image.
 */
export function setDragGhost(e: React.DragEvent, nodes: Node[]): void {
  if (typeof document === "undefined" || !e.dataTransfer?.setDragImage) return;

  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  const ghost = document.createElement("div");
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.position = "fixed";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.top = "-9999px";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.left = "-9999px";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.zIndex = "-1";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.maxWidth = "260px";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.whiteSpace = "nowrap";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.overflow = "hidden";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM element and CSS property values
  ghost.style.textOverflow = "ellipsis";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind CSS class string
  ghost.className =
    "pointer-events-none inline-flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground shadow-md";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- brief drag ghost label
  ghost.textContent = nodes.length === 1 ? nodes[0].name : `${nodes.length} items`;
  document.body.appendChild(ghost);
  e.dataTransfer.setDragImage(ghost, 12, 12);
  setTimeout(() => {
    ghost.remove();
  }, 0);
}
