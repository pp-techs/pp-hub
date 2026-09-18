import type { FunctionComponent, SVGProps } from "react";
import DefaultFile from "~icons/vscode-icons/default-file";
import DefaultFolder from "~icons/vscode-icons/default-folder";
import FolderShared from "~icons/vscode-icons/folder-type-public";
import FileAi from "~icons/vscode-icons/file-type-ai";
import FileAudio from "~icons/vscode-icons/file-type-audio";
import FileBinary from "~icons/vscode-icons/file-type-binary";
import FileC from "~icons/vscode-icons/file-type-c";
import FileCpp from "~icons/vscode-icons/file-type-cpp";
import FileCsharp from "~icons/vscode-icons/file-type-csharp";
import FileCss from "~icons/vscode-icons/file-type-css";
import FileEps from "~icons/vscode-icons/file-type-eps";
import FileEpub from "~icons/vscode-icons/file-type-epub";
import FileExcel from "~icons/vscode-icons/file-type-excel";
import FileFont from "~icons/vscode-icons/file-type-font";
import FileGo from "~icons/vscode-icons/file-type-go";
import FileHtml from "~icons/vscode-icons/file-type-html";
import FileImage from "~icons/vscode-icons/file-type-image";
import FileIni from "~icons/vscode-icons/file-type-ini";
import FileJava from "~icons/vscode-icons/file-type-java";
import FileJs from "~icons/vscode-icons/file-type-js";
import FileJson from "~icons/vscode-icons/file-type-json";
import FileJupyter from "~icons/vscode-icons/file-type-jupyter";
import FileKotlin from "~icons/vscode-icons/file-type-kotlin";
import FileLog from "~icons/vscode-icons/file-type-log";
import FileLua from "~icons/vscode-icons/file-type-lua";
import FileMarkdown from "~icons/vscode-icons/file-type-markdown";
import FilePdf from "~icons/vscode-icons/file-type-pdf2";
import FilePhp from "~icons/vscode-icons/file-type-php";
import FilePhotoshop from "~icons/vscode-icons/file-type-photoshop";
import FilePowerpoint from "~icons/vscode-icons/file-type-powerpoint";
import FilePython from "~icons/vscode-icons/file-type-python";
import FileR from "~icons/vscode-icons/file-type-r";
import FileReactJs from "~icons/vscode-icons/file-type-reactjs";
import FileReactTs from "~icons/vscode-icons/file-type-reactts";
import FileRuby from "~icons/vscode-icons/file-type-ruby";
import FileRust from "~icons/vscode-icons/file-type-rust";
import FileScss from "~icons/vscode-icons/file-type-scss";
import FileShell from "~icons/vscode-icons/file-type-shell";
import FileSql from "~icons/vscode-icons/file-type-sql";
import FileSvg from "~icons/vscode-icons/file-type-svg";
import FileSwift from "~icons/vscode-icons/file-type-swift";
import FileText from "~icons/vscode-icons/file-type-text";
import FileToml from "~icons/vscode-icons/file-type-toml";
import FileTypescript from "~icons/vscode-icons/file-type-typescript";
import FileVideo from "~icons/vscode-icons/file-type-video";
import FileVue from "~icons/vscode-icons/file-type-vue";
import FileWord from "~icons/vscode-icons/file-type-word";
import FileXml from "~icons/vscode-icons/file-type-xml";
import FileYaml from "~icons/vscode-icons/file-type-yaml";
import FileZip from "~icons/vscode-icons/file-type-zip";
import { cn } from "@workspace/ui/lib/utils";
import type { Node } from "@/lib/api";

type IconComponent = FunctionComponent<SVGProps<SVGSVGElement>>;

/**
 * Extension → glyph, straight out of the `vscode-icons` set
 * (https://icon-sets.iconify.design/vscode-icons/). The glyphs carry their own
 * colours, so callers size them and never tint them.
 *
 * Only the extension is consulted: the server's MIME sniffing is coarser than
 * the set is (every Office file is one of a handful of zip MIMEs) and a name is
 * always present, while `mime` is not.
 */
const BY_EXT: Record<string, IconComponent> = {
  // Documents
  pdf: FilePdf,
  doc: FileWord,
  docx: FileWord,
  odt: FileWord,
  rtf: FileWord,
  xls: FileExcel,
  xlsx: FileExcel,
  ods: FileExcel,
  csv: FileExcel,
  tsv: FileExcel,
  ppt: FilePowerpoint,
  pptx: FilePowerpoint,
  odp: FilePowerpoint,
  epub: FileEpub,
  txt: FileText,
  md: FileMarkdown,
  mdx: FileMarkdown,
  log: FileLog,

  // Media
  png: FileImage,
  jpg: FileImage,
  jpeg: FileImage,
  gif: FileImage,
  webp: FileImage,
  avif: FileImage,
  bmp: FileImage,
  ico: FileImage,
  tif: FileImage,
  tiff: FileImage,
  heic: FileImage,
  svg: FileSvg,
  psd: FilePhotoshop,
  ai: FileAi,
  eps: FileEps,
  mp4: FileVideo,
  webm: FileVideo,
  mov: FileVideo,
  avi: FileVideo,
  mkv: FileVideo,
  mp3: FileAudio,
  wav: FileAudio,
  flac: FileAudio,
  ogg: FileAudio,
  m4a: FileAudio,
  aac: FileAudio,

  // Archives & binaries
  zip: FileZip,
  rar: FileZip,
  "7z": FileZip,
  tar: FileZip,
  gz: FileZip,
  tgz: FileZip,
  bz2: FileZip,
  xz: FileZip,
  zst: FileZip,
  exe: FileBinary,
  dll: FileBinary,
  dmg: FileBinary,
  iso: FileBinary,
  bin: FileBinary,
  ttf: FileFont,
  otf: FileFont,
  woff: FileFont,
  woff2: FileFont,

  // Code & config
  json: FileJson,
  jsonc: FileJson,
  json5: FileJson,
  yaml: FileYaml,
  yml: FileYaml,
  toml: FileToml,
  ini: FileIni,
  conf: FileIni,
  env: FileIni,
  xml: FileXml,
  html: FileHtml,
  htm: FileHtml,
  css: FileCss,
  scss: FileScss,
  sass: FileScss,
  js: FileJs,
  mjs: FileJs,
  cjs: FileJs,
  jsx: FileReactJs,
  ts: FileTypescript,
  mts: FileTypescript,
  cts: FileTypescript,
  tsx: FileReactTs,
  vue: FileVue,
  py: FilePython,
  ipynb: FileJupyter,
  go: FileGo,
  rs: FileRust,
  java: FileJava,
  kt: FileKotlin,
  swift: FileSwift,
  rb: FileRuby,
  php: FilePhp,
  lua: FileLua,
  r: FileR,
  c: FileC,
  h: FileC,
  cc: FileCpp,
  cpp: FileCpp,
  hpp: FileCpp,
  cs: FileCsharp,
  sh: FileShell,
  bash: FileShell,
  zsh: FileShell,
  fish: FileShell,
  sql: FileSql,
};

/**
 * The `vscode-icons` glyph for a node: the set's default folder (or its
 * shared variant), or the glyph for the filename's extension, falling back
 * to its default file.
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
  if (node.kind === "dir") {
    const Folder = isShared ? FolderShared : DefaultFolder;
    return <Folder className={cls} />;
  }

  const dot = node.name.lastIndexOf(".");
  const Icon = (dot > 0 && BY_EXT[node.name.slice(dot + 1).toLowerCase()]) || DefaultFile;
  return <Icon className={cls} />;
}
