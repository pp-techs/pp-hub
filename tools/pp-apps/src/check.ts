import { generateGo, generateTs } from "./gen.ts";
import { loadConfig, PATHS, readJson, readText, resolveAll, type Resolved } from "./repo.ts";

/** Every way the tree disagrees with pp-apps.json; an empty list means it is consistent. Never throws on a bad tree. */
export function check(root: string): string[] {
  let config;
  try {
    config = loadConfig(root);
  } catch (e) {
    return (e as Error).message.split("\n");
  }
  const { resolved, problems } = resolveAll(root, config);

  // Ids and kinds are unique across extensions.
  const kindOwner: Record<string, string> = {};
  for (const e of resolved) {
    for (const { kind } of e.manifest.nodeKinds) {
      const other = kindOwner[kind];
      if (other !== undefined && other !== e.manifest.id) {
        problems.push(`node kind "${kind}" is declared by both "${other}" and "${e.manifest.id}"`);
      }
      kindOwner[kind] = e.manifest.id;
    }
  }

  // The web copy of an in-repo manifest is byte-identical to the canonical one.
  for (const e of resolved) {
    if (e.external || !e.webCopyPath) continue;
    const canonical = readText(root, e.manifestPath);
    const copy = readText(root, e.webCopyPath);
    if (copy === undefined) problems.push(`${e.webCopyPath}: missing; run "pp-apps sync"`);
    else if (copy !== canonical)
      problems.push(`${e.webCopyPath} differs from ${e.manifestPath}; run "pp-apps sync"`);
  }

  // The web app depends on every web package.
  const pkg = readJson(root, PATHS.webPackage);
  if (pkg.problem) problems.push(pkg.problem);
  else {
    const deps = dependenciesOf(pkg.value);
    for (const e of resolved) {
      if (!(e.manifest.web.package in deps)) {
        problems.push(`${PATHS.webPackage}: missing dependency "${e.manifest.web.package}"`);
      }
    }
  }

  // External entries pin the Go module in go.mod.
  const goMod = readText(root, PATHS.goMod) ?? "";
  for (const e of resolved) {
    const s = e.entry.server;
    if (
      "module" in s &&
      !new RegExp(
        `^\\s*(require\\s+)?${reEscape(s.module)}\\s+${reEscape(s.version)}\\b`,
        "m",
      ).test(goMod)
    ) {
      problems.push(`${PATHS.goMod}: does not require ${s.module} ${s.version}`);
    }
  }

  // Generated files are current.
  if (resolved.length === config.extensions.length) {
    generatedIsCurrent(root, resolved, problems);
  }
  return problems;
}

/** The `dependencies` keys of a parsed package.json. */
function dependenciesOf(pkg: unknown): Record<string, unknown> {
  if (typeof pkg === "object" && pkg !== null && "dependencies" in pkg) {
    const d = pkg.dependencies;
    if (typeof d === "object" && d !== null) return Object.fromEntries(Object.entries(d));
  }
  return {};
}

function reEscape(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function generatedIsCurrent(root: string, exts: Resolved[], problems: string[]) {
  const files: [string, string][] = [
    [PATHS.goGen, generateGo(exts)],
    [PATHS.tsGen, generateTs(exts)],
  ];
  for (const [rel, want] of files) {
    const have = readText(root, rel);
    if (have === undefined) problems.push(`${rel}: missing; run "pp-apps gen"`);
    else if (have !== want) problems.push(`${rel} is out of date; run "pp-apps gen"`);
  }
}
