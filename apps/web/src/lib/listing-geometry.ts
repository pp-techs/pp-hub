import type { Node } from "@/lib/api";
import type { MarqueeRect } from "@/lib/use-marquee-selection";

// The virtualized listings place rows from these numbers instead of measuring
// the DOM, and the marquee hit-tests against them, so each must match the
// Tailwind class the rendered element carries.

/** `gap-3` between cards, in both directions. */
export const GRID_GAP = 12;
/** `gap-6` between the Folders and Files sections. */
export const SECTION_GAP = 24;
/** GroupLabel's `h-4 mb-2`. */
export const LABEL_HEIGHT = 24;
/** A folder card's `h-12`. */
export const DIR_CARD_HEIGHT = 48;
/** A file card's `h-[198px]`. */
export const FILE_CARD_HEIGHT = 198;

/**
 * Columns for a grid this wide: Tailwind v4's `@lg`/`@2xl`/`@4xl` container
 * sizes (32rem, 42rem, 56rem), which the grid used before it was virtualized.
 * Tracking the grid's own width means opening the details panel drops 5
 * columns to 4 without the route having to tell the grid anything.
 */
export function gridColumns(width: number): number {
  if (width >= 896) return 5;
  if (width >= 672) return 4;
  if (width >= 512) return 3;
  return 2;
}

/** One virtual row of the grid: a section heading, or a run of cards. */
export type GridRow =
  | { key: string; kind: "label"; group: "dirs" | "files"; height: number }
  | { key: string; kind: "cards"; nodes: Node[]; cardHeight: number; height: number };

/**
 * The grid as rows of `cols` cards under a label per non-empty group, folders
 * first. A row's height carries the gap below it: the card gap inside a group,
 * the section gap after the last folder row when files follow, none at the end.
 */
export function buildGridRows(
  dirs: Node[],
  files: Node[],
  cols: number,
): { rows: GridRow[]; starts: number[]; total: number } {
  const rows: GridRow[] = [];
  const groups = [
    { group: "dirs" as const, nodes: dirs, cardHeight: DIR_CARD_HEIGHT },
    { group: "files" as const, nodes: files, cardHeight: FILE_CARD_HEIGHT },
  ];
  for (const { group, nodes, cardHeight } of groups) {
    if (nodes.length === 0) continue;
    // eslint-disable-next-line lingui/no-unlocalized-strings -- GridRow discriminant
    rows.push({ key: `label:${group}`, kind: "label", group, height: LABEL_HEIGHT });
    const last = Math.ceil(nodes.length / cols) - 1;
    const trailing = group === "dirs" && files.length > 0 ? SECTION_GAP : 0;
    for (let i = 0; i <= last; i++) {
      rows.push({
        key: `${group}:${i}`,
        // eslint-disable-next-line lingui/no-unlocalized-strings -- GridRow discriminant
        kind: "cards",
        nodes: nodes.slice(i * cols, (i + 1) * cols),
        cardHeight,
        height: cardHeight + (i === last ? trailing : GRID_GAP),
      });
    }
  }

  const starts: number[] = [];
  let total = 0;
  for (const row of rows) {
    starts.push(total);
    total += row.height;
  }
  return { rows, starts, total };
}

/**
 * The cards a band covers, with the band relative to the grid's content
 * origin. Overlap is strict, so a band that only touches a card's edge
 * leaves it out.
 */
export function hitTestGrid(
  layout: { rows: GridRow[]; starts: number[] },
  cols: number,
  width: number,
  band: MarqueeRect,
): Set<string> {
  const ids = new Set<string>();
  const right = band.left + band.width;
  const bottom = band.top + band.height;
  const colWidth = (width - GRID_GAP * (cols - 1)) / cols;

  layout.rows.forEach((row, i) => {
    if (row.kind !== "cards") return;
    const top = layout.starts[i]!;
    if (!(top < bottom && top + row.cardHeight > band.top)) return;
    row.nodes.forEach((node, c) => {
      const left = c * (colWidth + GRID_GAP);
      if (left < right && left + colWidth > band.left) ids.add(node.id);
    });
  });
  return ids;
}

/**
 * The rows a band covers, with the band relative to the tbody's origin. Rows
 * are all `rowHeight` tall, so the covered run is found by division rather
 * than by visiting each row. Overlap is strict, as in the grid.
 */
export function hitTestList(
  nodes: Node[],
  rowHeight: number,
  width: number,
  band: MarqueeRect,
): Set<string> {
  const ids = new Set<string>();
  if (!(band.left < width && band.left + band.width > 0)) return ids;
  const first = Math.max(0, Math.floor(band.top / rowHeight));
  const last = Math.min(nodes.length - 1, Math.ceil((band.top + band.height) / rowHeight) - 1);
  for (let i = first; i <= last; i++) ids.add(nodes[i]!.id);
  return ids;
}
