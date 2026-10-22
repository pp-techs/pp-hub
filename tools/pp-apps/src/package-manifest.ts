/** An app id: names its URLs, its notification kinds and its node kinds' prefix. */
export const ID_PATTERN = /^[a-z][a-z0-9_]{0,31}$/;
/** Semantic Versioning 2.0.0 (semver.org's reference expression). */
export const SEMVER_PATTERN =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$/;

/** The manifest of a packaged (uploadable) app; `packages/app-sdk/pp-app.schema.json` is its JSON Schema. */
export interface PackagedManifest {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  license?: string;
  homepage?: string;
  engines: { sdk: 1 };
  icon?: string;
  nodeKinds: PackagedKind[];
  web: { entry: string; connect?: string[] };
  backend?: { url: string };
  notifications?: Record<string, string>;
  translations?: Record<string, Record<string, string>>;
}

export interface PackagedKind {
  kind: string;
  label: string;
  newLabel?: string;
  defaultName?: string;
  newHint?: string;
  icon?: string;
  seed?: { collection: string; data: Record<string, unknown> }[];
}

/** The packaged-app API version (`engines.sdk`). */
export const PACKAGE_SDK = 1;
export const COLLECTION_PATTERN = /^[a-z][a-z0-9_]{0,31}$/;
const VERB_PATTERN = /^[a-z][a-z0-9_.-]{0,31}$/;
const LOCALE_PATTERN = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$/;
const MAX_SEED_BYTES = 64 * 1024;
const MAX_LOCALES = 50;
const MAX_STRINGS = 500;
const MAX_STRING_CHARS = 500;
/** "https://" + host[:port], nothing else. */
const BARE_ORIGIN = /^https:\/\/[^/?#@\s\\]+$/;

const TOP = [
  "id",
  "name",
  "version",
  "description",
  "author",
  "license",
  "homepage",
  "engines",
  "icon",
  "nodeKinds",
  "web",
  "backend",
  "notifications",
  "translations",
];
const REQUIRED = ["id", "name", "version", "description", "author", "engines", "nodeKinds", "web"];
const KIND_FIELDS = ["kind", "label", "newLabel", "defaultName", "newHint", "icon", "seed"];

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Length in code points, as the server counts runes. */
const length = (s: string) => Array.from(s).length;

/** Every file path the manifest references (icons and the web entry), for existence checks. */
export function referencedFiles(m: PackagedManifest): string[] {
  const files = [m.web.entry];
  if (m.icon) files.push(m.icon);
  for (const k of m.nodeKinds) if (k.icon) files.push(k.icon);
  return files;
}

/**
 * Validates a parsed packaged manifest against the rules of the packaged-app contract (the
 * same rules as the server's apppkg validator). `manifest` is the input when `problems` is empty.
 */
export function validatePackagedManifest(json: unknown): {
  manifest: PackagedManifest;
  problems: string[];
} {
  const out: string[] = [];
  const result = { manifest: json as PackagedManifest, problems: out };
  if (!isObject(json)) {
    out.push("pp-app.json: must be a JSON object");
    return result;
  }
  const web = json.web;

  const keys = (at: string, o: Record<string, unknown>, allowed: string[], required: string[]) => {
    for (const k of Object.keys(o))
      if (!allowed.includes(k)) out.push(`${at}: unknown field "${k}"`);
    for (const k of required) if (!(k in o)) out.push(`${at}: missing required field "${k}"`);
  };
  /** A required string: non-blank, at most `max` characters. */
  const text = (at: string, v: unknown, max: number) => {
    if (typeof v !== "string" || v.trim() === "") out.push(`${at}: must be a non-empty string`);
    else if (length(v) > max) out.push(`${at}: must be at most ${max} characters`);
  };
  /** An optional string: blank counts as absent. */
  const optText = (at: string, v: unknown, max: number) => {
    if (v === undefined) return;
    if (typeof v !== "string") out.push(`${at}: must be a string`);
    else if (length(v) > max) out.push(`${at}: must be at most ${max} characters`);
  };
  const httpURL = (at: string, v: unknown) => {
    let u: URL | undefined;
    try {
      u = typeof v === "string" ? new URL(v) : undefined;
    } catch {
      // reported below
    }
    if (!u || (u.protocol !== "http:" && u.protocol !== "https:") || !u.host) {
      out.push(`${at}: ${JSON.stringify(v)} must be an http(s) URL`);
      return undefined;
    }
    return u;
  };
  const filePath = (at: string, v: unknown, exts: string[]) => {
    if (typeof v !== "string" || v === "") return void out.push(`${at}: must be a file path`);
    if (
      v.startsWith("/") ||
      v.includes("\\") ||
      v.includes("\0") ||
      v.split("/").some((s) => s === ".." || s === "." || s === "")
    )
      return void out.push(
        `${at}: ${JSON.stringify(v)} must be a relative path inside the package`,
      );
    if (!exts.some((e) => v.endsWith(e)))
      out.push(`${at}: ${JSON.stringify(v)} must end with ${exts.join(" or ")}`);
  };

  keys("pp-app.json", json, TOP, REQUIRED);
  const id = json.id;
  if ("id" in json && (typeof id !== "string" || !ID_PATTERN.test(id)))
    out.push(`id: ${JSON.stringify(id)} must match ${ID_PATTERN.source}`);
  if ("name" in json) text("name", json.name, 60);
  if ("version" in json && (typeof json.version !== "string" || !SEMVER_PATTERN.test(json.version)))
    out.push(`version: ${JSON.stringify(json.version)} is not a semantic version (x.y.z)`);
  if ("description" in json) text("description", json.description, 300);
  if ("author" in json) text("author", json.author, 100);
  optText("license", json.license, 100);
  if (json.homepage !== undefined && json.homepage !== "") httpURL("homepage", json.homepage);

  if ("engines" in json) {
    if (!isObject(json.engines)) out.push("engines: must be an object");
    else {
      keys("engines", json.engines, ["sdk"], ["sdk"]);
      if ("sdk" in json.engines && json.engines.sdk !== PACKAGE_SDK)
        out.push(`engines.sdk: must be ${PACKAGE_SDK}`);
    }
  }
  if (json.icon !== undefined && json.icon !== "") filePath("icon", json.icon, [".svg", ".png"]);

  if ("nodeKinds" in json) {
    const kinds = json.nodeKinds;
    if (!Array.isArray(kinds) || kinds.length < 1 || kinds.length > 8)
      out.push("nodeKinds: must be an array of 1 to 8 kinds");
    else {
      const seen = new Set<string>();
      kinds.forEach((nk, i) => {
        const at = `nodeKinds[${i}]`;
        if (!isObject(nk)) return void out.push(`${at}: must be an object`);
        keys(at, nk, KIND_FIELDS, ["kind", "label"]);
        if ("kind" in nk) {
          const k = nk.kind;
          const prefix = typeof id === "string" ? `${id}.` : "<id>.";
          if (
            typeof k !== "string" ||
            !k.startsWith(prefix) ||
            !ID_PATTERN.test(k.slice(prefix.length))
          )
            out.push(
              `${at}.kind: ${JSON.stringify(k)} must be "${prefix}" followed by a name matching ${ID_PATTERN.source}`,
            );
          if (typeof k === "string") {
            if (seen.has(k)) out.push(`${at}.kind: ${JSON.stringify(k)} is listed twice`);
            seen.add(k);
          }
        }
        if ("label" in nk) text(`${at}.label`, nk.label, 60);
        optText(`${at}.newLabel`, nk.newLabel, 60);
        optText(`${at}.defaultName`, nk.defaultName, 120);
        optText(`${at}.newHint`, nk.newHint, 200);
        if (nk.icon !== undefined && nk.icon !== "")
          filePath(`${at}.icon`, nk.icon, [".svg", ".png"]);
        if (nk.seed !== undefined) {
          if (!Array.isArray(nk.seed) || nk.seed.length > 100)
            return void out.push(`${at}.seed: must be an array of at most 100 records`);
          nk.seed.forEach((s, j) => {
            const sat = `${at}.seed[${j}]`;
            if (!isObject(s)) return void out.push(`${sat}: must be an object`);
            keys(sat, s, ["collection", "data"], ["collection", "data"]);
            if (
              "collection" in s &&
              (typeof s.collection !== "string" || !COLLECTION_PATTERN.test(s.collection))
            )
              out.push(
                `${sat}.collection: ${JSON.stringify(s.collection)} must match ${COLLECTION_PATTERN.source}`,
              );
            if ("data" in s) {
              if (!isObject(s.data)) out.push(`${sat}.data: must be a JSON object`);
              else if (Buffer.byteLength(JSON.stringify(s.data)) > MAX_SEED_BYTES)
                out.push(`${sat}.data: must encode to at most 64 KiB`);
            }
          });
        }
      });
    }
  }

  if ("web" in json) {
    if (!isObject(web)) out.push("web: must be an object");
    else {
      keys("web", web, ["entry", "connect"], ["entry"]);
      if ("entry" in web) filePath("web.entry", web.entry, [".html"]);
      if (web.connect !== undefined) {
        if (!Array.isArray(web.connect) || web.connect.length > 10)
          out.push("web.connect: must be an array of at most 10 origins");
        else {
          const seen = new Set<string>();
          web.connect.forEach((o, i) => {
            // As the server: "https://" + host exactly — no user info, path, query or fragment.
            if (typeof o !== "string" || !BARE_ORIGIN.test(o) || !URL.canParse(o))
              out.push(
                `web.connect[${i}]: ${JSON.stringify(o)} must be a bare https origin like https://api.example.com`,
              );
            else if (seen.has(o))
              out.push(`web.connect[${i}]: ${JSON.stringify(o)} is listed twice`);
            if (typeof o === "string") seen.add(o);
          });
        }
      }
    }
  }

  if (json.backend !== undefined) {
    const b = json.backend;
    if (!isObject(b)) out.push("backend: must be an object");
    else {
      keys("backend", b, ["url"], ["url"]);
      const u = "url" in b ? httpURL("backend.url", b.url) : undefined;
      if (u?.username || u?.password) out.push("backend.url: must not contain user info");
      if (u && /[?#]/.test(b.url as string))
        out.push("backend.url: must not have a query or fragment");
    }
  }

  if (json.notifications !== undefined) {
    if (!isObject(json.notifications)) out.push("notifications: must be an object");
    else
      for (const [verb, tmpl] of Object.entries(json.notifications)) {
        if (!VERB_PATTERN.test(verb))
          out.push(`notifications: verb ${JSON.stringify(verb)} must match ${VERB_PATTERN.source}`);
        text(`notifications.${verb}`, tmpl, 200);
      }
  }

  if (json.translations !== undefined) {
    if (!isObject(json.translations)) out.push("translations: must be an object");
    else {
      const locales = Object.entries(json.translations);
      if (locales.length > MAX_LOCALES) out.push(`translations: at most ${MAX_LOCALES} locales`);
      for (const [locale, table] of locales) {
        if (!LOCALE_PATTERN.test(locale))
          out.push(
            `translations: locale ${JSON.stringify(locale)} must match ${LOCALE_PATTERN.source}`,
          );
        if (!isObject(table) || Object.values(table).some((v) => typeof v !== "string"))
          out.push(`translations.${locale}: must map English strings to translated strings`);
        else if (Object.keys(table).length > MAX_STRINGS)
          out.push(`translations.${locale}: at most ${MAX_STRINGS} strings`);
        else if (
          Object.entries(table).some(
            ([k, v]) => length(k) > MAX_STRING_CHARS || length(v as string) > MAX_STRING_CHARS,
          )
        )
          out.push(
            `translations.${locale}: strings must be at most ${MAX_STRING_CHARS} characters`,
          );
      }
    }
  }
  return result;
}
