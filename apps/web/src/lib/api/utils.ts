import type { Node } from "./types";
// Utilities shared across the app.

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}

const OFFICE_EXTS = new Set([
  ".docx",
  ".xlsx",
  ".pptx",
  ".doc",
  ".xls",
  ".csv",
  ".ppt",
  ".odt",
  ".ods",
  ".odp",
  ".rtf",
  ".epub",
]);
const OFFICE_MIMES = new Set([
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "application/msword",
  "application/vnd.ms-excel",
  "application/vnd.ms-powerpoint",
]);

/** Check by filename (e.g. "report.docx") or MIME type (e.g. "application/vnd...") */
export function isOfficeFile(nameOrMime?: string): boolean {
  if (!nameOrMime) return false;
  // If it looks like a MIME type (has "/"), check MIME set
  if (nameOrMime.includes("/")) return OFFICE_MIMES.has(nameOrMime);
  // Otherwise treat as filename and check extension
  const ext = nameOrMime.toLowerCase().match(/\.[^.]+$/)?.[0];
  return !!ext && OFFICE_EXTS.has(ext);
}

/** Open the office editor for a node in a new browser tab. */
export function openEditor(node: Node): void {
  window.open(`/edit/${node.id}`, "_blank");
}

export type PreviewKind = "thumbnail" | "pdf" | "video" | "text";

/**
 * Returns the preview kind the server generated for a MIME type, or null when
 * no preview exists for it.
 *
 * This MUST mirror kindForMime in apps/server/internal/preview/preview.go —
 * the worker stores blobs under the kind it picks, and GET
 * /nodes/:id/preview?kind=... 404s ("preview not ready") on any mismatch.
 */
export function previewKind(mime?: string): PreviewKind | null {
  if (!mime) return null;
  // Strip parameters, e.g. "text/plain; charset=utf-8".
  const m = mime.split(";")[0]!.trim();
  if (m === "application/pdf") return "pdf";
  if (m === "image/png" || m === "image/jpeg" || m === "image/gif" || m === "image/webp") {
    return "thumbnail";
  }
  if (m === "video/mp4" || m === "video/webm" || m === "video/quicktime") return "video";
  if (m === "application/rtf" || m.startsWith("text/")) return "text";
  return null;
}

/** File types that open in OnlyOffice on double-click. */
export function canEdit(node: Node): boolean {
  return node.kind === "file" && isOfficeFile(node.name);
}

/** File types the server can render a preview for. */
export function canPreview(node: Node): boolean {
  return node.kind === "file" && previewKind(node.mime) !== null;
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
