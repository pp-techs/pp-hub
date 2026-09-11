import type { Node } from "./types";
// Utilities shared across the app.

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}

export const OFFICE_EXT_RE = /\.(docx|xlsx|pptx|doc|xls|ppt|odt|ods|odp)$/i;

export function isOfficeFile(name: string): boolean {
  return OFFICE_EXT_RE.test(name);
}

/** Open the office editor for a node in a new browser tab. */
export function openEditor(node: Node): void {
  window.open(`/edit/${node.id}`, "_blank");
}

export type PreviewKind = "thumbnail" | "pdf" | "video" | "text";

export function previewKind(mime?: string): PreviewKind {
  if (mime?.startsWith("image/")) return "thumbnail";
  if (mime === "application/pdf") return "pdf";
  if (mime?.startsWith("video/")) return "video";
  return "text";
}

export type SortField = "name" | "modified" | "size";
export type SortOrder = "asc" | "desc";

/**
 * Sort nodes: folders always first, then by the active field + direction.
 */
export function sortNodes(nodes: Node[], field: SortField, order: SortOrder): Node[] {
  return [...nodes].sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === "dir" ? -1 : 1;
    let cmp = 0;
    switch (field) {
      case "name":
        cmp = a.name.localeCompare(b.name);
        break;
      case "modified":
        cmp = (a.modified_at ?? "").localeCompare(b.modified_at ?? "");
        break;
      case "size":
        cmp = (a.size ?? 0) - (b.size ?? 0);
        break;
    }
    return order === "asc" ? cmp : -cmp;
  });
}

/**
 * Format a relative-ish date string from an ISO timestamp.
 * Shows "Jan 2" for recent, "Dec 31, 2025" for older.
 */
export function fmtDate(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString(
    undefined,
    sameYear
      ? { month: "short", day: "numeric" }
      : { year: "numeric", month: "short", day: "numeric" },
  );
}
