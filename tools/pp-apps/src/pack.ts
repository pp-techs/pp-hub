import fs from "node:fs";
import path from "node:path";
import { zipSync, type Zippable } from "fflate";
import { referencedFiles, validatePackagedManifest } from "./package-manifest.ts";

const MiB = 1024 * 1024;
/** The package limits of the packaged-app contract. */
export const LIMITS = {
  zipBytes: 20 * MiB,
  files: 2000,
  fileBytes: 10 * MiB,
  totalBytes: 50 * MiB,
  iconBytes: 256 * 1024,
};

/** `src="/x"` / `href="/x"` (not protocol-relative `//host`): breaks under the versioned asset path. */
const ROOT_ABSOLUTE = /\b(?:src|href)\s*=\s*["']?\/(?!\/)/gi;

export interface PackResult {
  /** Absolute path of the written zip. */
  file: string;
  /** Package paths, sorted. */
  files: string[];
  /** Size of the zip. */
  bytes: number;
}

/** Package-relative paths of every file under `dir`, skipping node_modules, dot segments, zips and `exclude`. */
function walk(dir: string, exclude: string): string[] {
  const out: string[] = [];
  const visit = (abs: string, rel: string) => {
    for (const e of fs.readdirSync(abs, { withFileTypes: true })) {
      if (e.name.startsWith(".") || e.name === "node_modules") continue;
      const childAbs = path.join(abs, e.name);
      const childRel = rel ? `${rel}/${e.name}` : e.name;
      if (e.isDirectory()) visit(childAbs, childRel);
      else if (e.isSymbolicLink())
        throw new Error(`${childRel}: symlinks cannot be packaged; copy the file instead`);
      else if (e.isFile() && !e.name.endsWith(".zip") && childAbs !== exclude) out.push(childRel);
    }
  };
  visit(dir, "");
  return out.sort();
}

/** Validates the app in `dir` and writes its upload zip (default `<dir>/../<id>-<version>.zip`). */
export function pack(dir: string, output?: string): PackResult {
  const root = path.resolve(dir);
  const manifestPath = path.join(root, "pp-app.json");
  if (!fs.existsSync(manifestPath)) throw new Error(`${dir}: no pp-app.json`);
  let json: unknown;
  try {
    json = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch (e) {
    throw new Error(`pp-app.json: not valid JSON (${(e as Error).message})`);
  }
  const { manifest, problems } = validatePackagedManifest(json);
  if (problems.length) throw new Error(`invalid pp-app.json:\n  - ${problems.join("\n  - ")}`);

  const file = path.resolve(
    output ?? path.join(root, "..", `${manifest.id}-${manifest.version}.zip`),
  );
  const refs = referencedFiles(manifest);
  const entryDir = path.posix.dirname(manifest.web.entry);
  let files = walk(root, file);
  if (entryDir !== ".") {
    const keep = new Set(["pp-app.json", ...refs]);
    files = files.filter((f) => keep.has(f) || f.startsWith(`${entryDir}/`));
  }

  const errors: string[] = [];
  const present = new Set(files);
  for (const ref of refs)
    if (!present.has(ref)) errors.push(`${ref}: referenced in pp-app.json but not found`);
  if (files.length > LIMITS.files)
    errors.push(`${files.length} files; a package holds at most ${LIMITS.files}`);

  const zippable: Zippable = {};
  let total = 0;
  for (const f of files) {
    const body = fs.readFileSync(path.join(root, f));
    total += body.length;
    if (body.length > LIMITS.fileBytes) errors.push(`${f}: larger than 10 MiB`);
    zippable[f] = body;
  }
  if (total > LIMITS.totalBytes) errors.push("the files add up to more than 50 MiB");
  for (const icon of refs.filter((r) => r !== manifest.web.entry))
    if (present.has(icon) && (zippable[icon] as Uint8Array).length > LIMITS.iconBytes)
      errors.push(`${icon}: icons are at most 256 KiB`);

  if (present.has(manifest.web.entry)) {
    const html = Buffer.from(zippable[manifest.web.entry] as Uint8Array).toString("utf8");
    const bad = [...html.matchAll(ROOT_ABSOLUTE)].map((m) => {
      const line = html.slice(0, m.index).split("\n").length;
      return `${manifest.web.entry}:${line}`;
    });
    if (bad.length)
      errors.push(
        `${bad.join(", ")}: root-absolute asset reference; the app is served under a versioned path, so reference assets relatively ("./app.js"; with Vite set base: "./")`,
      );
  }
  if (errors.length) throw new Error(`cannot pack ${dir}:\n  - ${errors.join("\n  - ")}`);

  const zip = zipSync(zippable, { level: 9 });
  if (zip.length > LIMITS.zipBytes)
    throw new Error(`the zip is ${zip.length} bytes; uploads are at most 20 MiB`);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, zip);
  return { file, files, bytes: zip.length };
}
