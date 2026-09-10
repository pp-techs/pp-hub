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

export type PreviewKind = "thumbnail" | "pdf" | "video" | "text";

export function previewKind(mime?: string): PreviewKind {
  if (mime?.startsWith("image/")) return "thumbnail";
  if (mime === "application/pdf") return "pdf";
  if (mime?.startsWith("video/")) return "video";
  return "text";
}
