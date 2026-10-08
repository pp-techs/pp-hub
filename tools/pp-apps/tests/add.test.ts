import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { add, resolveExternal, splitSpec, type Run } from "../src/add.ts";
import { check } from "../src/check.ts";
import { loadConfig } from "../src/repo.ts";
import { json, makeRepo, manifestFor, put, read } from "./fixture.ts";

const MODULE = "github.com/acme/pp-notes";

/** Stands in for `go get` and `bun add`: lays an installed package into node_modules. */
function installer(root: string, over: Record<string, unknown> = {}): Run {
  return (cmd, args) => {
    if (cmd === "go") {
      put(
        root,
        "apps/server/go.mod",
        `${read(root, "apps/server/go.mod")}\nrequire ${MODULE} v1.2.0\n`,
      );
      return;
    }
    expect(args).toEqual(["add", "@acme/pp-notes@1.2.0"]);
    const m = manifestFor("notes", {
      server: { package: `${MODULE}/ext` },
      web: { package: "@acme/pp-notes" },
      ...over,
    });
    put(root, "node_modules/@acme/pp-notes/pp-app.json", json(m));
    const pkg = JSON.parse(read(root, "apps/web/package.json"));
    pkg.dependencies["@acme/pp-notes"] = "^1.2.0";
    put(root, "apps/web/package.json", json(pkg));
    put(
      root,
      "node_modules/@acme/pp-notes/package.json",
      json({ name: "@acme/pp-notes", version: "1.2.0", exports: { ".": "./index.ts" } }),
    );
  };
}

describe("splitSpec", () => {
  it("splits scoped and unscoped specs at the last @", () => {
    expect(splitSpec("@acme/pp-x@1.2.0", "p")).toEqual({ name: "@acme/pp-x", version: "1.2.0" });
    expect(splitSpec("github.com/a/b@v1.0.0", "m")).toEqual({
      name: "github.com/a/b",
      version: "v1.0.0",
    });
    expect(() => splitSpec("@acme/pp-x", "p")).toThrow(/<name>@<version>/);
    expect(() => splitSpec("pp-x@", "p")).toThrow();
  });
});

describe("resolveExternal (manifest resolution against a fixture package)", () => {
  it("reads node_modules/<package>/pp-app.json and builds the entry", () => {
    const root = makeRepo();
    installer(root)("bun", ["add", "@acme/pp-notes@1.2.0"], root);
    const { entry, manifest } = resolveExternal(
      root,
      { name: "@acme/pp-notes", version: "1.2.0" },
      { name: MODULE, version: "v1.2.0" },
    );
    expect(manifest.id).toBe("notes");
    expect(entry).toEqual({
      id: "notes",
      server: { module: MODULE, version: "v1.2.0" },
      web: { package: "@acme/pp-notes", version: "1.2.0" },
    });
  });

  it("rejects a Go package outside the module and a package that ships no manifest", () => {
    const root = makeRepo();
    installer(root, { server: { package: "github.com/evil/other" } })(
      "bun",
      ["add", "@acme/pp-notes@1.2.0"],
      root,
    );
    expect(() =>
      resolveExternal(
        root,
        { name: "@acme/pp-notes", version: "1.2.0" },
        { name: MODULE, version: "v1.2.0" },
      ),
    ).toThrow(/not under the Go module/);
    expect(() =>
      resolveExternal(
        root,
        { name: "@acme/missing", version: "1.0.0" },
        { name: MODULE, version: "v1.0.0" },
      ),
    ).toThrow(/does not ship a pp-app.json/);
  });

  it("rejects a manifest naming a different web package", () => {
    const root = makeRepo();
    installer(root, { web: { package: "@someone/else" } })(
      "bun",
      ["add", "@acme/pp-notes@1.2.0"],
      root,
    );
    expect(() =>
      resolveExternal(
        root,
        { name: "@acme/pp-notes", version: "1.2.0" },
        { name: MODULE, version: "v1.2.0" },
      ),
    ).toThrow(/does not match the installed package/);
  });
});

describe("add with stubbed installers", () => {
  it("registers the extension and generates; check then passes", () => {
    const root = makeRepo();
    const m = add(root, "@acme/pp-notes@1.2.0", `${MODULE}@v1.2.0`, installer(root));
    expect(m.id).toBe("notes");
    expect(loadConfig(root).extensions.at(-1)).toEqual({
      id: "notes",
      server: { module: MODULE, version: "v1.2.0" },
      web: { package: "@acme/pp-notes", version: "1.2.0" },
    });
    expect(read(root, "apps/server/cmd/server/extensions_gen.go")).toContain(
      `notes "${MODULE}/ext"`,
    );
    expect(read(root, "apps/web/src/ext/registry.gen.ts")).toContain(
      'import notesExt from "@acme/pp-notes";',
    );
    expect(check(root)).toEqual([]);
  });

  it("restores pp-apps.json and dependency files when validation fails", () => {
    const root = makeRepo();
    const before = ["pp-apps.json", "apps/web/package.json", "apps/server/go.mod"].map((f) =>
      read(root, f),
    );
    expect(() =>
      add(root, "@acme/pp-notes@1.2.0", `${MODULE}@v1.2.0`, installer(root, { id: "Bad Id" })),
    ).toThrow(/pp-apps add failed \(pp-apps.json and dependency files restored\)/);
    expect(
      ["pp-apps.json", "apps/web/package.json", "apps/server/go.mod"].map((f) => read(root, f)),
    ).toEqual(before);
  });

  it("restores when an install command fails", () => {
    const root = makeRepo();
    const before = read(root, "pp-apps.json");
    const failing: Run = () => {
      throw new Error("go get exited with 1");
    };
    expect(() => add(root, "@acme/pp-notes@1.2.0", `${MODULE}@v1.2.0`, failing)).toThrow(
      /go get exited with 1/,
    );
    expect(read(root, "pp-apps.json")).toBe(before);
    expect(fs.existsSync(path.join(root, "apps/server/cmd/server/extensions_gen.go"))).toBe(true);
  });
});
