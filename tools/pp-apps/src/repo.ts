import fs from "node:fs";
import path from "node:path";
import { idProblem, parseManifest, type Manifest } from "./manifest.ts";

/** Repo-relative locations the tool reads and writes. */
export const PATHS = {
  config: "pp-apps.json",
  serverModule: "apps/server",
  goMod: "apps/server/go.mod",
  goGen: "apps/server/cmd/server/extensions_gen.go",
  tsGen: "apps/web/src/ext/registry.gen.ts",
  webPackage: "apps/web/package.json",
  manifestFile: "pp-app.json",
} as const;

export type ServerRef = { dir: string } | { module: string; version: string };
export type WebRef = { dir: string } | { package: string; version: string };

export interface Entry {
  id: string;
  server: ServerRef;
  web: WebRef;
}

export interface Config {
  extensions: Entry[];
}

export interface Resolved {
  entry: Entry;
  manifest: Manifest;
  external: boolean;
  /** Where the manifest was read from, repo-relative. */
  manifestPath: string;
  /** In-repo only: the web package's copy, repo-relative. */
  webCopyPath?: string;
}

const abs = (root: string, rel: string) => path.join(root, rel);

export function readText(root: string, rel: string): string | undefined {
  try {
    return fs.readFileSync(abs(root, rel), "utf8");
  } catch {
    return undefined;
  }
}

export function readJson(root: string, rel: string): { value?: unknown; problem?: string } {
  const text = readText(root, rel);
  if (text === undefined) return { problem: `${rel}: cannot read` };
  try {
    return { value: JSON.parse(text) };
  } catch (e) {
    return { problem: `${rel}: not valid JSON (${(e as Error).message})` };
  }
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function exactKeys(at: string, o: Record<string, unknown>, keys: string[], out: string[]) {
  for (const k of Object.keys(o)) if (!keys.includes(k)) out.push(`${at}: unknown field "${k}"`);
  for (const k of keys)
    if (typeof o[k] !== "string" || o[k] === "") out.push(`${at}.${k}: must be a non-empty string`);
}

/** Every problem with a parsed pp-apps.json. */
export function validateConfig(value: unknown): string[] {
  const at = PATHS.config;
  const out: string[] = [];
  if (!isObject(value)) return [`${at}: must be a JSON object`];
  for (const k of Object.keys(value))
    if (k !== "extensions") out.push(`${at}: unknown field "${k}"`);
  if (!Array.isArray(value.extensions)) return [...out, `${at}.extensions: must be an array`];
  const ids = new Set<string>();
  value.extensions.forEach((e, i) => {
    const here = `${at}.extensions[${i}]`;
    if (!isObject(e)) return void out.push(`${here}: must be an object`);
    for (const k of Object.keys(e))
      if (!["id", "server", "web"].includes(k)) out.push(`${here}: unknown field "${k}"`);
    const p = idProblem(e.id);
    if (p) out.push(`${here}: ${p}`);
    else if (ids.has(e.id as string)) out.push(`${here}: id "${String(e.id)}" is listed twice`);
    ids.add(String(e.id));
    const { server, web } = e;
    if (!isObject(server) || !isObject(web))
      return void out.push(`${here}: "server" and "web" must be objects`);
    const inRepo = "dir" in server;
    if (inRepo !== "dir" in web) {
      return void out.push(
        `${here}: server and web must both be in-repo ({ "dir" }) or both external`,
      );
    }
    if (inRepo) {
      exactKeys(`${here}.server`, server, ["dir"], out);
      exactKeys(`${here}.web`, web, ["dir"], out);
    } else {
      exactKeys(`${here}.server`, server, ["module", "version"], out);
      exactKeys(`${here}.web`, web, ["package", "version"], out);
    }
  });
  return out;
}

export function loadConfig(root: string): Config {
  const { value, problem } = readJson(root, PATHS.config);
  if (problem) throw new Error(problem);
  const problems = validateConfig(value);
  if (problems.length) throw new Error(problems.join("\n"));
  return value as Config;
}

export function configText(config: Config): string {
  return `${JSON.stringify(config, null, 2)}\n`;
}

export function writeConfig(root: string, config: Config): void {
  fs.writeFileSync(abs(root, PATHS.config), configText(config));
}

/** The Go module path in apps/server/go.mod. */
export function goModule(root: string): string {
  const m = /^module\s+(\S+)/m.exec(readText(root, PATHS.goMod) ?? "");
  if (!m) throw new Error(`${PATHS.goMod}: no module line`);
  return m[1]!;
}

const posix = (p: string) => p.split(path.sep).join("/");

/** `node_modules/<pkg>/pp-app.json` as the web app resolves it, repo-relative. */
export function installedManifestPath(root: string, pkg: string): string {
  for (const base of ["apps/web/node_modules", "node_modules"]) {
    const rel = `${base}/${pkg}/${PATHS.manifestFile}`;
    if (fs.existsSync(abs(root, rel))) return rel;
  }
  return `node_modules/${pkg}/${PATHS.manifestFile}`;
}

/** Resolves one entry to its manifest, with every inconsistency between entry, manifest and packages. */
export function resolveEntry(
  root: string,
  entry: Entry,
): { resolved?: Resolved; problems: string[] } {
  const problems: string[] = [];
  const at = `extension "${entry.id}"`;
  const { server, web } = entry;

  if ("dir" in server && "dir" in web) {
    const manifestPath = posix(path.join(server.dir, PATHS.manifestFile));
    const text = readText(root, manifestPath);
    if (text === undefined) return { problems: [`${at}: ${manifestPath} not found`] };
    let manifest: Manifest;
    try {
      manifest = parseManifest(text, manifestPath);
    } catch (e) {
      return { problems: (e as Error).message.split("\n") };
    }
    if (manifest.id !== entry.id)
      problems.push(
        `${manifestPath}: id "${manifest.id}" does not match pp-apps.json entry "${entry.id}"`,
      );

    const rel = posix(path.relative(PATHS.serverModule, server.dir));
    if (rel.startsWith("..") || rel === "") {
      problems.push(`${at}: server.dir "${server.dir}" is not inside ${PATHS.serverModule}`);
    } else {
      const want = `${goModule(root)}/${rel}`;
      if (manifest.server.package !== want) {
        problems.push(
          `${manifestPath}: server.package "${manifest.server.package}" must be the Go import path "${want}"`,
        );
      }
    }

    const pkgJson = `${web.dir}/package.json`;
    const pkg = readJson(root, pkgJson);
    if (pkg.problem) problems.push(`${at}: ${pkg.problem}`);
    else if (isObject(pkg.value)) {
      if (pkg.value.name !== manifest.web.package) {
        problems.push(
          `${pkgJson}: name "${String(pkg.value.name)}" must equal web.package "${manifest.web.package}"`,
        );
      }
      const exp = pkg.value.exports;
      if (!isObject(exp) || !("." in exp)) problems.push(`${pkgJson}: must export "."`);
    }
    return {
      resolved: {
        entry,
        manifest,
        external: false,
        manifestPath,
        webCopyPath: `${web.dir}/${PATHS.manifestFile}`,
      },
      problems,
    };
  }

  if ("module" in server && "package" in web) {
    const manifestPath = installedManifestPath(root, web.package);
    const text = readText(root, manifestPath);
    if (text === undefined)
      return { problems: [`${at}: ${manifestPath} not found (is ${web.package} installed?)`] };
    let manifest: Manifest;
    try {
      manifest = parseManifest(text, manifestPath);
    } catch (e) {
      return { problems: (e as Error).message.split("\n") };
    }
    if (manifest.id !== entry.id)
      problems.push(
        `${manifestPath}: id "${manifest.id}" does not match pp-apps.json entry "${entry.id}"`,
      );
    if (manifest.web.package !== web.package) {
      problems.push(
        `${manifestPath}: web.package "${manifest.web.package}" does not match the installed package "${web.package}"`,
      );
    }
    const pkg = manifest.server.package;
    if (pkg !== server.module && !pkg.startsWith(`${server.module}/`)) {
      problems.push(
        `${manifestPath}: server.package "${pkg}" is not under the Go module "${server.module}"`,
      );
    }
    return { resolved: { entry, manifest, external: true, manifestPath }, problems };
  }
  return { problems: [`${at}: server and web must both be in-repo or both external`] };
}

/** Resolves every entry; `problems` is every inconsistency found, `resolved` the entries whose manifest could be read. */
export function resolveAll(
  root: string,
  config: Config,
): { resolved: Resolved[]; problems: string[] } {
  const resolved: Resolved[] = [];
  const problems: string[] = [];
  for (const entry of config.extensions) {
    const r = resolveEntry(root, entry);
    problems.push(...r.problems);
    if (r.resolved) resolved.push(r.resolved);
  }
  return { resolved, problems };
}
