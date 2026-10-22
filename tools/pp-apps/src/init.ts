import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ID_PATTERN } from "./package-manifest.ts";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const TEMPLATES = path.join(HERE, "..", "templates", "package");
/** The SDK copied into every new app: packages/app-sdk/src. */
export const SDK_DIR = path.join(HERE, "..", "..", "..", "packages", "app-sdk", "src");

/** Template (under templates/package/) → file in the new app. */
const LAYOUT: [string, string][] = [
  ["pp-app.json.tmpl", "pp-app.json"],
  ["index.html.tmpl", "index.html"],
  ["app.js.tmpl", "app.js"],
  ["style.css.tmpl", "style.css"],
  ["icon.svg.tmpl", "icon.svg"],
  ["README.md.tmpl", "README.md"],
];
/** SDK file → its name in the new app. */
const SDK_FILES: [string, string][] = [
  ["index.js", "app-sdk.js"],
  ["index.d.ts", "app-sdk.d.ts"],
];

/** Fills `{{name}}` placeholders; an unknown one is a template bug. */
function render(template: string, vars: Record<string, string>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => {
    const v = vars[key];
    if (v === undefined) throw new Error(`template uses unknown variable {{${key}}}`);
    return v;
  });
}

/** A directory name made into an app id: lowercase, [a-z0-9_], starting with a letter, ≤ 32 chars. */
export function idFromDir(dir: string): string {
  return path
    .basename(path.resolve(dir))
    .toLowerCase()
    .replace(/[^a-z0-9_]+/g, "_")
    .replace(/^[^a-z]+/, "")
    .slice(0, 32)
    .replace(/_+$/, "");
}

export interface InitResult {
  id: string;
  dir: string;
  files: string[];
  nextSteps: string[];
}

/** Scaffolds a zero-build packaged app into `dir` (absent or empty). Throws, writing nothing, when unusable. */
export function init(dir: string, opts: { id?: string; name?: string } = {}): InitResult {
  const target = path.resolve(dir);
  const id = opts.id ?? idFromDir(target);
  if (!ID_PATTERN.test(id))
    throw new Error(
      `id ${JSON.stringify(id)} must match ${ID_PATTERN.source}${opts.id ? "" : "; pass --id"}`,
    );
  const Name =
    opts.name ??
    id
      .split("_")
      .filter(Boolean)
      .map((w) => w[0]!.toUpperCase() + w.slice(1))
      .join(" ");
  if (/["\\<>{}]/.test(Name) || Name.trim() === "" || Array.from(Name).length > 50)
    throw new Error("--name must be plain text (≤ 50 characters, no quotes, backslashes, <>{})");
  if (
    fs.existsSync(target) &&
    (!fs.statSync(target).isDirectory() || fs.readdirSync(target).length)
  )
    throw new Error(`${dir} exists and is not an empty directory`);

  fs.mkdirSync(target, { recursive: true });
  const vars = { id, Name };
  for (const [tmpl, dest] of LAYOUT)
    fs.writeFileSync(
      path.join(target, dest),
      render(fs.readFileSync(path.join(TEMPLATES, tmpl), "utf8"), vars),
    );
  for (const [src, dest] of SDK_FILES)
    fs.copyFileSync(path.join(SDK_DIR, src), path.join(target, dest));

  return {
    id,
    dir: target,
    files: [...LAYOUT, ...SDK_FILES].map(([, dest]) => dest),
    nextSteps: [
      `edit ${path.join(dir, "pp-app.json")} (author, description) and ${path.join(dir, "app.js")}`,
      `bun run apps pack ${dir}`,
      "upload the zip in Admin › Apps",
    ],
  };
}
