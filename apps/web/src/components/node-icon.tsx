import {
  lazy,
  Suspense,
  type FunctionComponent,
  type LazyExoticComponent,
  type SVGProps,
} from "react";
import { Puzzle } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { extKindOf, useExtRegistry } from "@/ext/registry";
import type { Node } from "@/lib/api";

export type IconComponent = FunctionComponent<SVGProps<SVGSVGElement>>;
type IconLoader = () => Promise<{ default: IconComponent }>;

/**
 * Glyph name → loader, straight out of the `vscode-icons` set
 * (https://icon-sets.iconify.design/vscode-icons/). Each glyph is a separate
 * dynamic import so the ~50 multi-path icons stay out of the entry chunk and
 * load on demand instead. The glyphs carry their own colours, so callers size
 * them and never tint them.
 */
const LOADERS: Record<string, IconLoader> = {
  "default-file": () => import("~icons/vscode-icons/default-file"),
  "default-folder": () => import("~icons/vscode-icons/default-folder"),
  "default-folder-opened": () => import("~icons/vscode-icons/default-folder-opened"),
  "folder-type-public": () => import("~icons/vscode-icons/folder-type-public"),
  "file-type-ai": () => import("~icons/vscode-icons/file-type-ai"),
  "file-type-audio": () => import("~icons/vscode-icons/file-type-audio"),
  "file-type-binary": () => import("~icons/vscode-icons/file-type-binary"),
  "file-type-c": () => import("~icons/vscode-icons/file-type-c"),
  "file-type-cpp": () => import("~icons/vscode-icons/file-type-cpp"),
  "file-type-csharp": () => import("~icons/vscode-icons/file-type-csharp"),
  "file-type-css": () => import("~icons/vscode-icons/file-type-css"),
  "file-type-eps": () => import("~icons/vscode-icons/file-type-eps"),
  "file-type-epub": () => import("~icons/vscode-icons/file-type-epub"),
  "file-type-excel": () => import("~icons/vscode-icons/file-type-excel"),
  "file-type-font": () => import("~icons/vscode-icons/file-type-font"),
  "file-type-go": () => import("~icons/vscode-icons/file-type-go"),
  "file-type-html": () => import("~icons/vscode-icons/file-type-html"),
  "file-type-image": () => import("~icons/vscode-icons/file-type-image"),
  "file-type-ini": () => import("~icons/vscode-icons/file-type-ini"),
  "file-type-java": () => import("~icons/vscode-icons/file-type-java"),
  "file-type-js": () => import("~icons/vscode-icons/file-type-js"),
  "file-type-json": () => import("~icons/vscode-icons/file-type-json"),
  "file-type-jupyter": () => import("~icons/vscode-icons/file-type-jupyter"),
  "file-type-kotlin": () => import("~icons/vscode-icons/file-type-kotlin"),
  "file-type-log": () => import("~icons/vscode-icons/file-type-log"),
  "file-type-lua": () => import("~icons/vscode-icons/file-type-lua"),
  "file-type-markdown": () => import("~icons/vscode-icons/file-type-markdown"),
  "file-type-pdf2": () => import("~icons/vscode-icons/file-type-pdf2"),
  "file-type-php": () => import("~icons/vscode-icons/file-type-php"),
  "file-type-photoshop": () => import("~icons/vscode-icons/file-type-photoshop"),
  "file-type-powerpoint": () => import("~icons/vscode-icons/file-type-powerpoint"),
  "file-type-python": () => import("~icons/vscode-icons/file-type-python"),
  "file-type-r": () => import("~icons/vscode-icons/file-type-r"),
  "file-type-reactjs": () => import("~icons/vscode-icons/file-type-reactjs"),
  "file-type-reactts": () => import("~icons/vscode-icons/file-type-reactts"),
  "file-type-ruby": () => import("~icons/vscode-icons/file-type-ruby"),
  "file-type-rust": () => import("~icons/vscode-icons/file-type-rust"),
  "file-type-scss": () => import("~icons/vscode-icons/file-type-scss"),
  "file-type-shell": () => import("~icons/vscode-icons/file-type-shell"),
  "file-type-sql": () => import("~icons/vscode-icons/file-type-sql"),
  "file-type-svg": () => import("~icons/vscode-icons/file-type-svg"),
  "file-type-swift": () => import("~icons/vscode-icons/file-type-swift"),
  "file-type-text": () => import("~icons/vscode-icons/file-type-text"),
  "file-type-toml": () => import("~icons/vscode-icons/file-type-toml"),
  "file-type-typescript": () => import("~icons/vscode-icons/file-type-typescript"),
  "file-type-video": () => import("~icons/vscode-icons/file-type-video"),
  "file-type-vue": () => import("~icons/vscode-icons/file-type-vue"),
  "file-type-word": () => import("~icons/vscode-icons/file-type-word"),
  "file-type-xml": () => import("~icons/vscode-icons/file-type-xml"),
  "file-type-yaml": () => import("~icons/vscode-icons/file-type-yaml"),
  "file-type-zip": () => import("~icons/vscode-icons/file-type-zip"),
};

/** One lazy component per glyph, created on first use and cached. */
const LAZY = new Map<string, LazyExoticComponent<IconComponent>>();

function iconFor(name: string): LazyExoticComponent<IconComponent> {
  let icon = LAZY.get(name);
  if (!icon) {
    icon = lazy(LOADERS[name] ?? LOADERS["default-file"]!);
    LAZY.set(name, icon);
  }
  return icon;
}

/**
 * Extension → glyph name. Only the extension is consulted: the server's MIME
 * sniffing is coarser than the set is (every Office file is one of a handful
 * of zip MIMEs) and a name is always present, while `mime` is not.
 */
const BY_EXT: Record<string, string> = {
  // Documents
  pdf: "file-type-pdf2",
  doc: "file-type-word",
  docx: "file-type-word",
  odt: "file-type-word",
  rtf: "file-type-word",
  xls: "file-type-excel",
  xlsx: "file-type-excel",
  ods: "file-type-excel",
  csv: "file-type-excel",
  tsv: "file-type-excel",
  ppt: "file-type-powerpoint",
  pptx: "file-type-powerpoint",
  odp: "file-type-powerpoint",
  epub: "file-type-epub",
  txt: "file-type-text",
  md: "file-type-markdown",
  mdx: "file-type-markdown",
  log: "file-type-log",

  // Media
  png: "file-type-image",
  jpg: "file-type-image",
  jpeg: "file-type-image",
  gif: "file-type-image",
  webp: "file-type-image",
  avif: "file-type-image",
  bmp: "file-type-image",
  ico: "file-type-image",
  tif: "file-type-image",
  tiff: "file-type-image",
  heic: "file-type-image",
  svg: "file-type-svg",
  psd: "file-type-photoshop",
  ai: "file-type-ai",
  eps: "file-type-eps",
  mp4: "file-type-video",
  webm: "file-type-video",
  mov: "file-type-video",
  avi: "file-type-video",
  mkv: "file-type-video",
  mp3: "file-type-audio",
  wav: "file-type-audio",
  flac: "file-type-audio",
  ogg: "file-type-audio",
  m4a: "file-type-audio",
  aac: "file-type-audio",

  // Archives & binaries
  zip: "file-type-zip",
  rar: "file-type-zip",
  "7z": "file-type-zip",
  tar: "file-type-zip",
  gz: "file-type-zip",
  tgz: "file-type-zip",
  bz2: "file-type-zip",
  xz: "file-type-zip",
  zst: "file-type-zip",
  exe: "file-type-binary",
  dll: "file-type-binary",
  dmg: "file-type-binary",
  iso: "file-type-binary",
  bin: "file-type-binary",
  ttf: "file-type-font",
  otf: "file-type-font",
  woff: "file-type-font",
  woff2: "file-type-font",

  // Code & config
  json: "file-type-json",
  jsonc: "file-type-json",
  json5: "file-type-json",
  yaml: "file-type-yaml",
  yml: "file-type-yaml",
  toml: "file-type-toml",
  ini: "file-type-ini",
  conf: "file-type-ini",
  env: "file-type-ini",
  xml: "file-type-xml",
  html: "file-type-html",
  htm: "file-type-html",
  css: "file-type-css",
  scss: "file-type-scss",
  sass: "file-type-scss",
  js: "file-type-js",
  mjs: "file-type-js",
  cjs: "file-type-js",
  jsx: "file-type-reactjs",
  ts: "file-type-typescript",
  mts: "file-type-typescript",
  cts: "file-type-typescript",
  tsx: "file-type-reactts",
  vue: "file-type-vue",
  py: "file-type-python",
  ipynb: "file-type-jupyter",
  go: "file-type-go",
  rs: "file-type-rust",
  java: "file-type-java",
  kt: "file-type-kotlin",
  swift: "file-type-swift",
  rb: "file-type-ruby",
  php: "file-type-php",
  lua: "file-type-lua",
  r: "file-type-r",
  c: "file-type-c",
  h: "file-type-c",
  cc: "file-type-cpp",
  cpp: "file-type-cpp",
  hpp: "file-type-cpp",
  cs: "file-type-csharp",
  sh: "file-type-shell",
  bash: "file-type-shell",
  zsh: "file-type-shell",
  fish: "file-type-shell",
  sql: "file-type-sql",
};

/** Glyph → static favicon file in `public/favicons/`. Only the kinds a tab can be editing. */
const FAVICONS: Record<string, string> = {
  "file-type-word": "/favicons/word.svg",
  "file-type-excel": "/favicons/excel.svg",
  "file-type-powerpoint": "/favicons/powerpoint.svg",
  "file-type-markdown": "/favicons/markdown.svg",
};

/**
 * The tab icon for a file being edited: the same glyph the listing shows,
 * for Word, Excel, PowerPoint and Markdown. `undefined` for anything else,
 * which keeps the product favicon.
 */
export function faviconFor(name: string): string | undefined {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return undefined;
  const glyph = BY_EXT[name.slice(dot + 1).toLowerCase()];
  return glyph ? FAVICONS[glyph] : undefined;
}

/**
 * The glyph a file called `name` would wear — used where an action names the
 * file it is about to create and no node exists yet (the "New …" items in the
 * folder menus). Only the extension is consulted, as in `NodeIcon`, so the menu
 * shows exactly the icon the created file will later get in the listing.
 *
 * The glyph loads lazily and the fallback is the same empty svg as `NodeIcon`'s,
 * so a menu that opens before the glyph lands does not shift.
 */
export function extIcon(name: string): IconComponent {
  const dot = name.lastIndexOf(".");
  const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : name.toLowerCase();
  const Icon = iconFor(BY_EXT[ext] ?? "default-file");
  return function ExtIcon({ className }: SVGProps<SVGSVGElement>) {
    return (
      <Suspense fallback={<svg className={className} aria-hidden="true" />}>
        <Icon className={className} aria-hidden="true" />
      </Suspense>
    );
  };
}

/**
 * The `vscode-icons` glyph for a node: the set's default folder, or the glyph
 * for the filename's extension, falling back to its default file.
 *
 * Folders come in three: a team folder wears `folder-type-public` — it has no
 * personal owner, the people it is granted to hold it — a folder shared with
 * others is `default-folder-opened`, and anything else is `default-folder`.
 */
export function NodeIcon({
  node,
  className,
  isShared,
}: {
  node: Node;
  className?: string;
  isShared?: boolean;
}) {
  const cls = cn("size-4 shrink-0", className);
  // An uploaded app's glyph arrives with the extensions list.
  useExtRegistry();
  // The fallback is an empty svg with the same class list, so a pending glyph
  // occupies the exact box the loaded one will — no layout shift in lists.
  const fallback = <svg className={cls} aria-hidden="true" />;

  if (node.kind === "dir") {
    const Folder = iconFor(
      node.team_folder
        ? "folder-type-public"
        : isShared
          ? "default-folder-opened"
          : "default-folder",
    );
    return (
      <Suspense fallback={fallback}>
        <Folder className={cls} />
      </Suspense>
    );
  }

  // An extension node wears its registered kind's glyph; a kind this build
  // does not carry gets a generic one rather than a file icon it is not.
  if (node.kind === "ext") {
    const Glyph = extKindOf(node.ext_kind)?.icon ?? Puzzle;
    return <Glyph className={cls} aria-hidden="true" />;
  }

  const dot = node.name.lastIndexOf(".");
  const Icon = iconFor(
    (dot > 0 && BY_EXT[node.name.slice(dot + 1).toLowerCase()]) || "default-file",
  );
  return (
    <Suspense fallback={fallback}>
      <Icon className={cls} />
    </Suspense>
  );
}
