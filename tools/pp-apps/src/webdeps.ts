import fs from "node:fs";
import path from "node:path";
import { PATHS } from "./repo.ts";

type Json = Record<string, unknown>;

/** Edits apps/web/package.json's `dependencies` (kept sorted) and writes it back in the file's 2-space form. */
function editDependencies(root: string, edit: (deps: Record<string, unknown>) => void): void {
  const file = path.join(root, PATHS.webPackage);
  const pkg: Json = JSON.parse(fs.readFileSync(file, "utf8"));
  const current = pkg.dependencies;
  const deps: Record<string, unknown> =
    typeof current === "object" && current !== null
      ? Object.fromEntries(Object.entries(current))
      : {};
  edit(deps);
  pkg.dependencies = Object.fromEntries(
    Object.entries(deps).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
  );
  fs.writeFileSync(file, `${JSON.stringify(pkg, null, 2)}\n`);
}

export function addWebDependency(root: string, name: string, range: string): void {
  editDependencies(root, (d) => {
    d[name] = range;
  });
}

export function removeWebDependency(root: string, name: string): void {
  editDependencies(root, (d) => {
    delete d[name];
  });
}
