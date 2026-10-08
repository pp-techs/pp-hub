/** The extension manifest `pp-app.json`; `packages/ext-sdk/pp-app.schema.json` is its JSON Schema. */
export interface Manifest {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  license?: string;
  homepage?: string;
  engines: { sdk: number };
  nodeKinds: { kind: string }[];
  server: { package: string };
  web: { package: string };
}

export const ID_PATTERN = /^[a-z][a-z0-9_]{0,31}$/;
/** Semantic Versioning 2.0.0 (semver.org's reference expression). */
export const SEMVER_PATTERN =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$/;

const GO_KEYWORDS = new Set(
  "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var".split(
    " ",
  ),
);

/** Why an id cannot be an extension id, or undefined. It doubles as a Go import alias, so Go keywords and the names `main`/`extsdk` are out. */
export function idProblem(id: unknown): string | undefined {
  if (typeof id !== "string" || !ID_PATTERN.test(id)) {
    return `id ${JSON.stringify(id)} must match ${ID_PATTERN.source}`;
  }
  if (GO_KEYWORDS.has(id) || id === "main" || id === "extsdk") {
    return `id "${id}" cannot be used: it is a Go keyword or a name the generated server file already uses`;
  }
  return undefined;
}

const TOP = [
  "id",
  "name",
  "version",
  "description",
  "author",
  "license",
  "homepage",
  "engines",
  "nodeKinds",
  "server",
  "web",
];
const REQUIRED = [
  "id",
  "name",
  "version",
  "description",
  "author",
  "engines",
  "nodeKinds",
  "server",
  "web",
];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function checkKeys(
  at: string,
  o: Record<string, unknown>,
  allowed: string[],
  required: string[],
  out: string[],
) {
  for (const k of Object.keys(o)) if (!allowed.includes(k)) out.push(`${at}: unknown field "${k}"`);
  for (const k of required) if (!(k in o)) out.push(`${at}: missing required field "${k}"`);
}

function checkString(at: string, v: unknown, out: string[], opts: { optional?: boolean } = {}) {
  if (v === undefined && opts.optional) return;
  if (typeof v !== "string" || v.trim() === "") out.push(`${at}: must be a non-empty string`);
}

/** Every problem with a parsed manifest, empty when it is valid. `at` prefixes each message. */
export function validateManifest(value: unknown, at = "manifest"): string[] {
  const out: string[] = [];
  if (!isObject(value)) return [`${at}: must be a JSON object`];
  checkKeys(at, value, TOP, REQUIRED, out);

  const id = value.id;
  if ("id" in value) {
    const p = idProblem(id);
    if (p) out.push(`${at}: ${p}`);
  }
  checkString(`${at}.name`, value.name, out);
  if (
    "version" in value &&
    (typeof value.version !== "string" || !SEMVER_PATTERN.test(value.version))
  ) {
    out.push(`${at}.version: ${JSON.stringify(value.version)} is not a semantic version (x.y.z)`);
  }
  // The admin page shows the description, so it may not be blank.
  checkString(`${at}.description`, value.description, out);
  checkString(`${at}.author`, value.author, out);
  checkString(`${at}.license`, value.license, out, { optional: true });
  checkString(`${at}.homepage`, value.homepage, out, { optional: true });

  if ("engines" in value) {
    if (!isObject(value.engines)) out.push(`${at}.engines: must be an object`);
    else {
      checkKeys(`${at}.engines`, value.engines, ["sdk"], ["sdk"], out);
      const sdk = value.engines.sdk;
      if ("sdk" in value.engines && (!Number.isInteger(sdk) || (sdk as number) < 1)) {
        out.push(`${at}.engines.sdk: must be an integer >= 1`);
      }
    }
  }

  if ("nodeKinds" in value) {
    if (!Array.isArray(value.nodeKinds)) out.push(`${at}.nodeKinds: must be an array`);
    else {
      const seen = new Set<string>();
      value.nodeKinds.forEach((nk, i) => {
        const here = `${at}.nodeKinds[${i}]`;
        if (!isObject(nk)) return void out.push(`${here}: must be an object`);
        checkKeys(here, nk, ["kind"], ["kind"], out);
        if (typeof nk.kind !== "string") {
          if ("kind" in nk) out.push(`${here}.kind: must be a string`);
          return;
        }
        const prefix = typeof id === "string" ? `${id}.` : undefined;
        if (!/^[a-z][a-z0-9_]{0,31}\.[^\s.]+$/.test(nk.kind)) {
          out.push(`${here}.kind: ${JSON.stringify(nk.kind)} must look like "<id>.<name>"`);
        } else if (prefix && !nk.kind.startsWith(prefix)) {
          out.push(`${here}.kind: ${JSON.stringify(nk.kind)} must start with "${prefix}"`);
        }
        if (seen.has(nk.kind)) out.push(`${here}.kind: ${JSON.stringify(nk.kind)} is listed twice`);
        seen.add(nk.kind);
      });
    }
  }

  for (const side of ["server", "web"] as const) {
    if (!(side in value)) continue;
    const o = value[side];
    if (!isObject(o)) out.push(`${at}.${side}: must be an object`);
    else {
      checkKeys(`${at}.${side}`, o, ["package"], ["package"], out);
      if ("package" in o) checkString(`${at}.${side}.package`, o.package, out);
    }
  }
  return out;
}

/** Parse manifest text; throws an Error listing every problem. */
export function parseManifest(text: string, at: string): Manifest {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (e) {
    throw new Error(`${at}: not valid JSON (${(e as Error).message})`);
  }
  const problems = validateManifest(value, at);
  if (problems.length) throw new Error(problems.join("\n"));
  return value as Manifest;
}
