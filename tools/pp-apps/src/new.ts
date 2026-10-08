import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { gen } from "./gen.ts";
import { idProblem } from "./manifest.ts";
import { addWebDependency } from "./webdeps.ts";
import { goModule, loadConfig, PATHS, writeConfig } from "./repo.ts";

const TEMPLATES = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "templates");

/** Template file (under templates/) to its destination (repo-relative), given the template variables. */
function layout(v: Vars): [string, string][] {
  const server = `apps/server/ext/${v.id}`;
  const web = `packages/ext-${v.id}`;
  return [
    ["server/pp-app.json.tmpl", `${server}/pp-app.json`],
    ["server/ext.go.tmpl", `${server}/${v.id}.go`],
    ["server/handlers.go.tmpl", `${server}/handlers.go`],
    ["server/migrations/0001_init.sql.tmpl", `${server}/migrations/0001_init.sql`],
    ["web/package.json.tmpl", `${web}/package.json`],
    ["web/tsconfig.json.tmpl", `${web}/tsconfig.json`],
    ["web/src/index.ts.tmpl", `${web}/src/index.ts`],
    ["web/src/editor.tsx.tmpl", `${web}/src/${v.slug}-editor.tsx`],
  ];
}

type Vars = {
  id: string;
  Name: string;
  Pascal: string;
  slug: string;
  serverPackage: string;
  webPackage: string;
};

function render(template: string, vars: Record<string, string>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => {
    const v = vars[key];
    if (v === undefined) throw new Error(`template uses unknown variable {{${key}}}`);
    return v;
  });
}

const words = (id: string) => id.split("_").filter(Boolean);

export interface NewResult {
  files: string[];
  nextSteps: string[];
}

/** Scaffolds an in-repo extension, registers it and runs gen. Throws, touching nothing, when the id is unusable. */
export function scaffold(root: string, id: string, name?: string): NewResult {
  const bad = idProblem(id);
  if (bad) throw new Error(bad);
  const config = loadConfig(root);
  if (config.extensions.some((e) => e.id === id))
    throw new Error(`extension "${id}" is already in ${PATHS.config}`);

  const vars: Vars = {
    id,
    Name:
      name ??
      words(id)
        .map((w) => w[0]!.toUpperCase() + w.slice(1))
        .join(" "),
    Pascal: words(id)
      .map((w) => w[0]!.toUpperCase() + w.slice(1))
      .join(""),
    slug: words(id).join("-"),
    serverPackage: `${goModule(root)}/ext/${id}`,
    webPackage: `@pp-hub/ext-${id}`,
  };
  if (/["\\`]/.test(vars.Name) || vars.Name.trim() === "")
    throw new Error("--name must be plain text without quotes or backslashes");

  const files = layout(vars);
  for (const dest of [`apps/server/ext/${id}`, `packages/ext-${id}`]) {
    if (fs.existsSync(path.join(root, dest))) throw new Error(`${dest} already exists`);
  }

  for (const [tmpl, dest] of files) {
    const target = path.join(root, dest);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, render(fs.readFileSync(path.join(TEMPLATES, tmpl), "utf8"), vars));
  }

  config.extensions.push({
    id,
    server: { dir: `apps/server/ext/${id}` },
    web: { dir: `packages/ext-${id}` },
  });
  writeConfig(root, config);
  addWebDependency(root, vars.webPackage, "workspace:*");
  gen(root);

  return {
    files: files.map(([, dest]) => dest),
    nextSteps: [
      "bun install",
      "(cd apps/server && go build ./...)",
      `edit apps/server/ext/${id}/pp-app.json (author, description) and run: bun run apps gen`,
      "bun run apps check",
    ],
  };
}
