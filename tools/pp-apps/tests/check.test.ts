import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { check } from "../src/check.ts";
import { gen, sync } from "../src/gen.ts";
import { loadConfig, resolveAll } from "../src/repo.ts";
import { removeWebDependency } from "../src/webdeps.ts";
import { json, makeRepo, put, putExtension, read, registerInRepo } from "./fixture.ts";

describe("check", () => {
  it("passes on a generated tree and gen is idempotent", () => {
    const root = makeRepo();
    expect(check(root)).toEqual([]);
    expect(gen(root)).toEqual([]);
  });

  it("detects a web manifest copy that drifted, and sync repairs it", () => {
    const root = makeRepo();
    fs.appendFileSync(path.join(root, "packages/ext-kanban/pp-app.json"), " ");
    expect(check(root).join("\n")).toContain(
      "packages/ext-kanban/pp-app.json differs from apps/server/ext/kanban/pp-app.json",
    );
    const { resolved } = resolveAll(root, loadConfig(root));
    expect(sync(root, resolved)).toEqual(["packages/ext-kanban/pp-app.json"]);
    expect(check(root)).toEqual([]);
  });

  it("detects stale generated files", () => {
    const root = makeRepo();
    fs.appendFileSync(path.join(root, "apps/web/src/ext/registry.gen.ts"), "// edited\n");
    fs.rmSync(path.join(root, "apps/server/cmd/server/extensions_gen.go"));
    const out = check(root).join("\n");
    expect(out).toContain("registry.gen.ts is out of date");
    expect(out).toContain("extensions_gen.go: missing");
  });

  it("detects a changed registration list", () => {
    const root = makeRepo();
    putExtension(root, "extra");
    registerInRepo(root, ["kanban", "checklist", "extra"]);
    const out = check(root).join("\n");
    expect(out).toContain('missing dependency "@pp-hub/ext-extra"');
    expect(out).toContain("extensions_gen.go is out of date");
  });

  it("detects a missing web dependency", () => {
    const root = makeRepo();
    removeWebDependency(root, "@pp-hub/ext-checklist");
    expect(check(root)).toEqual([
      'apps/web/package.json: missing dependency "@pp-hub/ext-checklist"',
    ]);
  });

  it("detects duplicate node kinds across extensions", () => {
    const root = makeRepo();
    const file = "apps/server/ext/checklist/pp-app.json";
    const m = JSON.parse(read(root, file));
    m.nodeKinds = [{ kind: "checklist.thing" }, { kind: "checklist.thing" }];
    put(root, file, json(m));
    expect(check(root).join("\n")).toContain("listed twice");
  });

  it("detects an id that disagrees with its entry, and a wrong Go import path", () => {
    const root = makeRepo();
    const file = "apps/server/ext/kanban/pp-app.json";
    const m = JSON.parse(read(root, file));
    m.server.package = "github.com/nhatbui/pp-hub/apps/server/ext/other";
    put(root, file, json(m));
    expect(check(root).join("\n")).toContain(
      'server.package "github.com/nhatbui/pp-hub/apps/server/ext/other" must be the Go import path',
    );
  });

  it("detects a web package whose name or exports are wrong", () => {
    const root = makeRepo();
    put(root, "packages/ext-kanban/package.json", json({ name: "@pp-hub/ext-wrong" }));
    const out = check(root).join("\n");
    expect(out).toContain('name "@pp-hub/ext-wrong" must equal web.package "@pp-hub/ext-kanban"');
    expect(out).toContain('must export "."');
  });

  it("rejects unknown manifest fields", () => {
    const root = makeRepo();
    const file = "apps/server/ext/kanban/pp-app.json";
    put(root, file, json({ ...JSON.parse(read(root, file)), surprise: 1 }));
    expect(check(root).join("\n")).toContain('unknown field "surprise"');
  });

  it("reports a malformed pp-apps.json instead of throwing", () => {
    const root = makeRepo();
    put(root, "pp-apps.json", "{");
    expect(check(root)[0]).toContain("pp-apps.json: not valid JSON");
  });
});
