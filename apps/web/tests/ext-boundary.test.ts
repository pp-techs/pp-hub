import { describe, expect, it } from "vite-plus/test";

/**
 * An extension's web package is a plugin, not a part of the app: it may import
 * the SDK, a short list of libraries the host shares with it, and its own
 * files — never `@/…`, never `apps/*`. This is the web twin of the Go
 * `TestExtImportsOnlySDK`: the boundary is a test, not a convention.
 *
 * Sources are read through Vite's raw glob, as `catalogs.test.ts` does: the
 * app's tsconfig does not pull in Node types.
 */
const SOURCES = import.meta.glob("../../../packages/ext-*/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

/** What an extension package may import: package names, with any subpath (`@lingui/react/macro`). */
const EXTENSION_ALLOWED = [
  "react",
  "react-dom",
  "@tanstack/react-query",
  "@lingui/core",
  "@lingui/react",
  "lucide-react",
  "sonner",
  "@workspace/ui",
  "@pp-hub/ext-sdk",
];

/** The SDK is the one package that may reach for what it re-exports for the others. */
const SDK_ALLOWED = [...EXTENSION_ALLOWED, "@buiducnhat/better-modal"];

const SDK_PACKAGE = "ext-sdk";

/** Every module specifier a source file imports or re-exports, static or dynamic. */
function specifiers(source: string): string[] {
  const found: string[] = [];
  const patterns = [
    /\bfrom\s*["']([^"']+)["']/g, // import x from "y", export * from "y"
    /^\s*import\s*["']([^"']+)["']/gm, // import "y"
    /\bimport\s*\(\s*["']([^"']+)["']\s*\)/g, // import("y")
    /\brequire\s*\(\s*["']([^"']+)["']\s*\)/g,
  ];
  for (const pattern of patterns) {
    for (const m of source.matchAll(pattern)) found.push(m[1]!);
  }
  return found;
}

/** `a/b/../c` → `a/c`, keeping leading `..` (the keys start outside the app). */
function normalize(path: string): string {
  const out: string[] = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === ".." && out.length > 0 && out[out.length - 1] !== "..") out.pop();
    else out.push(part);
  }
  return out.join("/");
}

const named = (spec: string, pkg: string) => spec === pkg || spec.startsWith(`${pkg}/`);

/** Why `spec`, imported by `file`, is not allowed; undefined when it is. */
function violation(file: string, spec: string): string | undefined {
  const root = /^(.*\/packages\/(ext-[^/]+))\//.exec(file);
  if (!root) return `${file} is not under packages/ext-*`;
  const [, pkgRoot, pkg] = root;

  if (spec.startsWith(".")) {
    const target = normalize(`${file.slice(0, file.lastIndexOf("/"))}/${spec}`);
    return target.startsWith(`${normalize(pkgRoot!)}/`)
      ? undefined
      : `a relative import that leaves the package (${spec})`;
  }
  if (spec.startsWith("@/")) return `the app's alias (${spec}); go through @pp-hub/ext-sdk`;
  if (/(^|\/)apps\//.test(spec)) return `the app's code (${spec})`;

  const allowed = pkg === SDK_PACKAGE ? SDK_ALLOWED : EXTENSION_ALLOWED;
  if (spec === "@workspace/ui") return "@workspace/ui needs a subpath (@workspace/ui/components/…)";
  return allowed.some((name) => named(spec, name))
    ? undefined
    : `"${spec}", which is not on the allow-list (${allowed.join(", ")})`;
}

/** The messages for every offending import in `sources`, one per line. */
function boundaryViolations(sources: Record<string, string>): string[] {
  const out: string[] = [];
  for (const [file, source] of Object.entries(sources)) {
    for (const spec of specifiers(source)) {
      const why = violation(file, spec);
      if (why) out.push(`${file.replace(/^(\.\.\/)+/, "")} imports ${why}`);
    }
  }
  return out;
}

describe("extension packages import only the SDK", () => {
  it("scans the SDK and both built-in extensions", () => {
    const packages = new Set(
      Object.keys(SOURCES).map((f) => /packages\/(ext-[^/]+)\//.exec(f)?.[1]),
    );
    expect(packages).toContain("ext-sdk");
    expect(packages).toContain("ext-kanban");
    expect(packages).toContain("ext-checklist");
  });

  it("finds no import outside the allow-list", () => {
    const found = boundaryViolations(SOURCES);
    expect(
      found,
      `Extension packages may import only: ${EXTENSION_ALLOWED.join(", ")}, relative files and their own pp-app.json.\n${found.join("\n")}`,
    ).toEqual([]);
  });

  it("recognises a violation", () => {
    const base = "../../../packages/ext-kanban/src/x.ts";
    const bad = (code: string) => boundaryViolations({ [base]: code }).length;
    expect(bad(`import { api } from "@/lib/api";`)).toBe(1);
    expect(bad(`import ky from "ky";`)).toBe(1);
    expect(bad(`import "../../../apps/web/src/main";`)).toBe(1);
    expect(bad(`const m = await import("zustand");`)).toBe(1);
    expect(bad(`import x from "../../ext-checklist/src/api";`)).toBe(1);
    expect(bad(`import { Button } from "@workspace/ui/components/button";`)).toBe(0);
    expect(bad(`import { msg } from "@lingui/core/macro";\nimport m from "../pp-app.json";`)).toBe(
      0,
    );
    expect(
      boundaryViolations({
        "../../../packages/ext-sdk/src/x.ts": `import "@buiducnhat/better-modal";`,
      }),
    ).toEqual([]);
    expect(bad(`import "@buiducnhat/better-modal";`)).toBe(1);
  });
});
