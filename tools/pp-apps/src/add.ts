import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { gen } from "./gen.ts";
import { parseManifest, type Manifest } from "./manifest.ts";
import {
  installedManifestPath,
  loadConfig,
  PATHS,
  readText,
  resolveEntry,
  writeConfig,
  type Entry,
} from "./repo.ts";

/** `name@version` where name may be scoped (`@acme/pp-x@1.2.0`). */
export function splitSpec(spec: string, what: string): { name: string; version: string } {
  const at = spec.lastIndexOf("@");
  if (at <= 0 || at === spec.length - 1)
    throw new Error(`${what} "${spec}" must be <name>@<version>`);
  return { name: spec.slice(0, at), version: spec.slice(at + 1) };
}

/**
 * Builds the external entry for an installed package and checks it against
 * the manifest it ships: ids agree, the package is the one named, and the Go
 * package lies under the module. Pure file reads: the install already ran.
 */
export function resolveExternal(
  root: string,
  web: { name: string; version: string },
  server: { name: string; version: string },
): { entry: Entry; manifest: Manifest } {
  const manifestPath = installedManifestPath(root, web.name);
  const text = readText(root, manifestPath);
  if (text === undefined)
    throw new Error(`${manifestPath} not found: ${web.name} does not ship a pp-app.json`);
  const manifest = parseManifest(text, manifestPath);
  const entry: Entry = {
    id: manifest.id,
    server: { module: server.name, version: server.version },
    web: { package: web.name, version: web.version },
  };
  const { problems } = resolveEntry(root, entry);
  if (problems.length) throw new Error(problems.join("\n"));
  return { entry, manifest };
}

export type Run = (cmd: string, args: string[], cwd: string) => void;

const run: Run = (cmd, args, cwd) => {
  const r = spawnSync(cmd, args, { cwd, stdio: "inherit" });
  if (r.error) throw new Error(`${cmd} ${args.join(" ")}: ${r.error.message}`);
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(" ")} exited with ${r.status}`);
};

const RESTORE = [PATHS.config, PATHS.webPackage, PATHS.goMod, "apps/server/go.sum", "bun.lock"];

/**
 * Installs an external extension: `go get`, `bun add`, a pp-apps.json entry,
 * then gen. On any failure the files those steps touch are put back.
 * The network steps (`go get`, `bun add`) are only as good as the registry
 * behind them; `runner` exists so tests can stand in for them.
 */
export function add(root: string, webSpec: string, goSpec: string, runner: Run = run): Manifest {
  const web = splitSpec(webSpec, "web package");
  const server = splitSpec(goSpec, "Go module");
  const saved = RESTORE.map((rel) => [rel, readText(root, rel)] as const);
  try {
    runner("go", ["get", `${server.name}@${server.version}`], path.join(root, PATHS.serverModule));
    runner("bun", ["add", `${web.name}@${web.version}`], path.join(root, "apps/web"));
    const { entry, manifest } = resolveExternal(root, web, server);
    const config = loadConfig(root);
    if (config.extensions.some((e) => e.id === entry.id))
      throw new Error(`extension "${entry.id}" is already in ${PATHS.config}`);
    config.extensions.push(entry);
    writeConfig(root, config);
    gen(root);
    return manifest;
  } catch (e) {
    for (const [rel, text] of saved) {
      if (text !== undefined) fs.writeFileSync(path.join(root, rel), text);
    }
    throw new Error(
      `pp-apps add failed (pp-apps.json and dependency files restored): ${(e as Error).message}`,
    );
  }
}
