import { gen } from "./gen.ts";
import { loadConfig, PATHS, resolveAll, writeConfig } from "./repo.ts";
import { removeWebDependency } from "./webdeps.ts";

/** Unregisters an extension: its pp-apps.json entry and web dependency, then regenerates. Its files stay. */
export function remove(root: string, id: string): void {
  const config = loadConfig(root);
  const entry = config.extensions.find((e) => e.id === id);
  if (!entry) throw new Error(`extension "${id}" is not in ${PATHS.config}`);
  // The web package name is needed for the dependency; read it from the entry or, in-repo, its package.json.
  const web = entry.web;
  let pkg: string | undefined = "package" in web ? web.package : undefined;
  if (!pkg) {
    const { resolved } = resolveAll(root, { extensions: [entry] });
    pkg = resolved[0]?.manifest.web.package;
  }
  config.extensions = config.extensions.filter((e) => e.id !== id);
  writeConfig(root, config);
  if (pkg) removeWebDependency(root, pkg);
  gen(root);
}
