import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { gen } from "../src/gen.ts";
import type { Manifest } from "../src/manifest.ts";

export const GO_MODULE = "github.com/nhatbui/pp-hub/apps/server";

export function manifestFor(id: string, over: Partial<Manifest> = {}): Manifest {
  return {
    id,
    name: id[0]!.toUpperCase() + id.slice(1),
    version: "1.0.0",
    description: `The ${id} app.`,
    author: "pp-hub",
    engines: { sdk: 1 },
    nodeKinds: [{ kind: `${id}.thing` }],
    server: { package: `${GO_MODULE}/ext/${id}` },
    web: { package: `@pp-hub/ext-${id}` },
    ...over,
  };
}

export function put(root: string, rel: string, text: string) {
  const file = path.join(root, rel);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, text);
}

export const json = (v: unknown) => `${JSON.stringify(v, null, 2)}\n`;

/** Writes one in-repo extension's files (without registering it). */
export function putExtension(root: string, id: string, over: Partial<Manifest> = {}) {
  const m = manifestFor(id, over);
  put(root, `apps/server/ext/${id}/pp-app.json`, json(m));
  put(
    root,
    `packages/ext-${id}/package.json`,
    json({ name: m.web.package, exports: { ".": "./src/index.ts" } }),
  );
  put(root, `packages/ext-${id}/src/index.ts`, "export default {};\n");
}

export function registerInRepo(root: string, ids: string[]) {
  put(
    root,
    "pp-apps.json",
    json({
      extensions: ids.map((id) => ({
        id,
        server: { dir: `apps/server/ext/${id}` },
        web: { dir: `packages/ext-${id}` },
      })),
    }),
  );
}

/** A temp copy of the repo layout pp-apps cares about, with the given extensions registered and generated. */
export function makeRepo(ids: string[] = ["kanban", "checklist"]): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pp-apps-"));
  put(root, "apps/server/go.mod", `module ${GO_MODULE}\n\ngo 1.26\n`);
  put(root, "apps/web/package.json", json({ name: "web", dependencies: { react: "^19" } }));
  for (const id of ids) putExtension(root, id);
  registerInRepo(root, ids);
  const deps = Object.fromEntries(ids.map((id) => [`@pp-hub/ext-${id}`, "workspace:*"]));
  put(
    root,
    "apps/web/package.json",
    json({ name: "web", dependencies: { react: "^19", ...deps } }),
  );
  gen(root);
  return root;
}

export const read = (root: string, rel: string) => fs.readFileSync(path.join(root, rel), "utf8");
