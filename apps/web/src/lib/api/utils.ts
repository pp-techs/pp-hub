import type { Node } from "./types";
// Utilities shared across the app.

// Byte-multiple symbols are written the same way in every locale we ship;
// what differs is the decimal separator, which Intl.NumberFormat owns.
const SIZE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

// Building an Intl formatter costs far more than formatting with one, and a
// listing formats a size and a date per row on every render — so each is
// built once per locale and kept. Keyed by `locale ?? ""`; the constructor
// still gets the caller's `locale`, so `undefined` keeps meaning "default".
const sizeFormats = new Map<string, Intl.NumberFormat>();
const dateFormats = new Map<string, { short: Intl.DateTimeFormat; long: Intl.DateTimeFormat }>();
const relativeFormats = new Map<string, Intl.RelativeTimeFormat>();

function sizeFormat(locale: string | undefined): Intl.NumberFormat {
  const key = locale ?? "";
  let fmt = sizeFormats.get(key);
  if (!fmt) {
    fmt = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
    sizeFormats.set(key, fmt);
  }
  return fmt;
}

function dateFormat(locale: string | undefined) {
  const key = locale ?? "";
  let fmt = dateFormats.get(key);
  if (!fmt) {
    fmt = {
      short: new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }),
      long: new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric" }),
    };
    dateFormats.set(key, fmt);
  }
  return fmt;
}

function relativeFormat(locale: string | undefined): Intl.RelativeTimeFormat {
  const key = locale ?? "";
  let fmt = relativeFormats.get(key);
  if (!fmt) {
    fmt = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
    relativeFormats.set(key, fmt);
  }
  return fmt;
}

export function fmtSize(n: number, locale?: string): string {
  if (n < 1024) return `${n} B`;
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < SIZE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const num = sizeFormat(locale).format(value);
  return `${num} ${SIZE_UNITS[unit]}`;
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

/**
 * A markdown document, which opens in the block editor. By the stored MIME
 * when the server recorded one — it carries a charset (`text/markdown;
 * charset=utf-8`), so only the base type is compared — else by extension.
 */
export function isMarkdownFile(node: Node): boolean {
  if (node.kind !== "file") return false;
  const mime = node.mime?.split(";")[0]?.trim();
  return mime === "text/markdown" || node.name.toLowerCase().endsWith(".md");
}

/**
 * Source and plain-text extensions the previewer shows as text, with the
 * highlight.js language to colour them in (`null`: shown plain). Decided by
 * name, not by MIME: the server's type comes from the host's mime.types, which
 * calls `.go` or `.py` application/octet-stream on a slim container and `.ts`
 * video/mp2t on a full one. Only extensions that are text in practice belong
 * here — `.svg` stays an image.
 */
const TEXT_EXTENSIONS: Record<string, string | null> = {
  ".txt": null,
  ".log": null,
  ".env": null,
  ".gitignore": null,
  ".dockerignore": null,
  ".editorconfig": null,
  ".csv": null,
  ".tsv": null,
  ".js": "javascript",
  ".mjs": "javascript",
  ".cjs": "javascript",
  ".jsx": "javascript",
  ".ts": "typescript",
  ".mts": "typescript",
  ".cts": "typescript",
  ".tsx": "typescript",
  ".json": "json",
  ".jsonc": "json",
  ".json5": "json",
  ".yaml": "yaml",
  ".yml": "yaml",
  ".toml": "ini",
  ".ini": "ini",
  ".cfg": "ini",
  ".conf": "ini",
  ".properties": "ini",
  ".xml": "xml",
  ".html": "xml",
  ".htm": "xml",
  ".xhtml": "xml",
  ".vue": "xml",
  ".svelte": "xml",
  ".css": "css",
  ".scss": "scss",
  ".sass": "scss",
  ".less": "less",
  ".py": "python",
  ".go": "go",
  ".rs": "rust",
  ".java": "java",
  ".kt": "kotlin",
  ".kts": "kotlin",
  ".swift": "swift",
  ".c": "c",
  ".h": "c",
  ".cc": "cpp",
  ".cpp": "cpp",
  ".cxx": "cpp",
  ".hpp": "cpp",
  ".m": "objectivec",
  ".cs": "csharp",
  ".vb": "vbnet",
  ".php": "php",
  ".rb": "ruby",
  ".pl": "perl",
  ".lua": "lua",
  ".r": "r",
  ".sql": "sql",
  ".graphql": "graphql",
  ".gql": "graphql",
  ".sh": "bash",
  ".bash": "bash",
  ".zsh": "bash",
  ".diff": "diff",
  ".patch": "diff",
  ".mk": "makefile",
};

/** Extensionless files that are text, by lower-cased name. */
const TEXT_NAMES: Record<string, string | null> = {
  makefile: "makefile",
  dockerfile: "bash",
  license: null,
  readme: null,
  changelog: null,
};

/** Structured-text types the server stores as `application/*`. */
const TEXT_MIMES: Record<string, string | null> = {
  "application/json": "json",
  "application/xml": "xml",
  "application/javascript": "javascript",
  "application/x-javascript": "javascript",
  "application/typescript": "typescript",
  "application/x-yaml": "yaml",
  "application/yaml": "yaml",
  "application/toml": "ini",
  "application/sql": "sql",
  "application/x-sh": "bash",
  "application/x-httpd-php": "php",
};

/** Where a text file's language comes from: its name first, then its MIME. */
function textEntry(node: Node): { language: string | null } | null {
  if (node.kind !== "file") return null;
  const name = node.name.toLowerCase();
  const ext = name.match(/\.[^.]+$/)?.[0];
  if (ext && ext in TEXT_EXTENSIONS) return { language: TEXT_EXTENSIONS[ext]! };
  if (!ext && name in TEXT_NAMES) return { language: TEXT_NAMES[name]! };
  const mime = node.mime?.split(";")[0]?.trim().toLowerCase();
  if (!mime) return null;
  if (mime in TEXT_MIMES) return { language: TEXT_MIMES[mime]! };
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("text/")) return { language: null };
  return null;
}

/**
 * A file the previewer shows as text — plain text, config, source code — read
 * from `/content`. Markdown is not one: it renders as a document.
 */
export function isTextFile(node: Node): boolean {
  return !isMarkdownFile(node) && textEntry(node) !== null;
}

/** The highlight.js language to colour a text file in, or null for plain. */
export function textLanguage(node: Node): string | null {
  return textEntry(node)?.language ?? null;
}

/** Open the editor for a node — OnlyOffice or the block editor, by type — in a new browser tab. */
export function openEditor(node: Pick<Node, "id">): void {
  window.open(`/edit/${node.id}`, "_blank");
}

/**
 * URL for the file's own bytes, served inline so the browser renders them
 * (PDF viewer, <img>, <video>) instead of downloading. Distinct from
 * `/preview`, which serves the small blob the preview worker generated.
 */
export function contentUrl(node: Node): string {
  return `/api/v1/nodes/${node.id}/content`;
}

/**
 * MIME types the server is willing to serve inline. Anything absent here is
 * forced to a download, so linking it as "open in a tab" would be a lie.
 *
 * Mirrors inlineMimes in apps/server/internal/httpapi/drive.go.
 */
const INLINE_MIMES = new Set([
  "application/pdf",
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "video/mp4",
  "video/webm",
  "video/quicktime",
  "audio/mpeg",
  "audio/mp4",
  "audio/ogg",
  "audio/wav",
  "text/plain",
]);

/** File types the browser can display straight from `/content`. */
export function canOpenInTab(node: Node): boolean {
  if (node.kind !== "file" || !node.mime) return false;
  return INLINE_MIMES.has(node.mime.split(";")[0]!.trim());
}

/** Open the file itself in a new browser tab (PDFs, images, video). */
export function openInTab(node: Node): void {
  window.open(contentUrl(node), "_blank", "noopener,noreferrer");
}

export type PreviewKind = "thumbnail" | "text";

/**
 * Returns the preview *artifact* the server stores for a MIME type, or null
 * when it stores none.
 *
 * This is deliberately not a mirror of kindForMime in
 * apps/server/internal/preview/preview.go. That function picks a *job* kind —
 * how the file gets rendered, which is the worker's business. PDFs, videos and
 * images all rasterize to the same artifact, so they all answer "thumbnail"
 * here. See docs/architecture/preview-pipeline.md.
 */
const THUMBNAIL_MIMES = new Set([
  "application/pdf",
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  // Not in INLINE_MIMES — /content downgrades SVG to attachment since it can
  // embed scripts. The thumbnail is a rasterized PNG, so it carries none of
  // that risk (apps/server/internal/preview/generators.go).
  "image/svg+xml",
  "video/mp4",
  "video/webm",
  "video/quicktime",
  // Office documents, rendered via OnlyOffice conversion. Narrower than the
  // set the editor opens: no octet-stream fallback, no .csv and no .epub.
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/msword",
  "application/vnd.oasis.opendocument.text",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "application/vnd.ms-excel",
  "application/vnd.oasis.opendocument.spreadsheet",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "application/vnd.ms-powerpoint",
  "application/vnd.oasis.opendocument.presentation",
]);

export function previewKind(mime?: string): PreviewKind | null {
  if (!mime) return null;
  // Strip parameters, e.g. "text/plain; charset=utf-8".
  const m = mime.split(";")[0]!.trim();
  if (THUMBNAIL_MIMES.has(m)) return "thumbnail";
  if (m === "application/rtf" || m.startsWith("text/")) return "text";
  return null;
}

/**
 * URL for a node's generated preview.
 *
 * The version is part of the URL because the response is cached `immutable`
 * for a year: editing a file has to change the URL, or every viewer keeps the
 * superseded thumbnail until their cache expires.
 */
export function previewUrl(node: Node, kind: PreviewKind = "thumbnail"): string {
  const v = node.version ?? 0;
  return `/api/v1/nodes/${node.id}/preview?kind=${kind}&v=${v}`;
}

/** Files the server renders a thumbnail for — the grid and details panel. */
export function hasThumbnail(node: Node): boolean {
  return node.kind === "file" && previewKind(node.mime) === "thumbnail";
}

/**
 * How a thumbnail should fill its frame: pictures crop, pages letterbox.
 *
 * A photo or a video poster frame is recognizable from any part of it, so
 * filling the frame looks deliberate and the wall of cards stays even. A page
 * is not: a portrait A4 cropped to a landscape box shows a horizontal strip of
 * its middle, which is the part that identifies a document least. PDFs sit
 * with office files here rather than with images — what matters is that it is
 * a page, not how it was rendered.
 *
 * Returns the semantic fit; callers map it to a literal class, since Tailwind
 * cannot see a class name assembled at runtime.
 */
export function thumbnailFit(node: Node): "cover" | "contain" {
  const m = node.mime?.split(";")[0]?.trim() ?? "";
  return m.startsWith("image/") || m.startsWith("video/") ? "cover" : "contain";
}

/**
 * What opens in an editor tab on double-click: office files in OnlyOffice,
 * markdown in the block editor, and every extension node in its extension's editor.
 */
export function opensInEditor(node: Node): boolean {
  if (node.kind === "ext") return true;
  return node.kind === "file" && (isOfficeFile(node.name) || isMarkdownFile(node));
}

/**
 * File types the preview modal can show something for — either the browser
 * renders the original inline, or the server stored a blob we can display.
 */
export function canPreview(node: Node): boolean {
  if (node.kind !== "file") return false;
  return canOpenInTab(node) || previewKind(node.mime) !== null || isTextFile(node);
}

export type SortField = "name" | "modified" | "size";
export type SortOrder = "asc" | "desc";

/**
 * Sort nodes: folders always first, then by the active field + direction.
 *
 * `locale` reaches `localeCompare` because Vietnamese collates diacritics
 * ("Đ" after "D", not after "Z") and the host's default locale is not
 * necessarily the one the app is being read in.
 */
export function sortNodes(
  nodes: Node[],
  field: SortField,
  order: SortOrder,
  locale?: string,
): Node[] {
  const collator = new Intl.Collator(locale);
  return [...nodes].sort((a, b) => {
    // An extension node sorts with the files: it is an item, not a container.
    if ((a.kind === "dir") !== (b.kind === "dir")) return a.kind === "dir" ? -1 : 1;
    let cmp = 0;
    switch (field) {
      case "name":
        cmp = collator.compare(a.name, b.name);
        break;
      case "modified":
        // ISO-8601 timestamps sort lexicographically; this is an ordering of
        // machine strings, not of text a reader sees, so it stays locale-free.
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
const RELATIVE_STEPS: [limit: number, unit: Intl.RelativeTimeFormatUnit, per: number][] = [
  [60, "second", 1],
  [3600, "minute", 60],
  [86400, "hour", 3600],
  [604800, "day", 86400],
];

/**
 * "2 minutes ago" for anything inside the last week, falling back to an
 * absolute date beyond it — past a few days "17 days ago" is harder to read
 * than the date itself.
 */
export function fmtRelative(iso: string | undefined, locale?: string): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const elapsed = (Date.now() - then) / 1000;
  if (elapsed >= 604800) return fmtDate(iso, locale);

  const rtf = relativeFormat(locale);
  // `numeric: "auto"` renders a zero offset as the locale's own word for it
  // ("now", "bây giờ"), so this branch needs no string of its own — which is
  // what keeps the transport layer free of translatable text.
  if (elapsed < 45) return rtf.format(0, "second");
  for (const [limit, unit, per] of RELATIVE_STEPS) {
    if (elapsed < limit) return rtf.format(-Math.round(elapsed / per), unit);
  }
  return fmtDate(iso, locale);
}

export function fmtDate(iso: string | undefined, locale?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  const fmt = dateFormat(locale);
  return (sameYear ? fmt.short : fmt.long).format(d);
}
