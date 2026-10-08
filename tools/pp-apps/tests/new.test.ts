import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { check } from "../src/check.ts";
import { remove } from "../src/remove.ts";
import { scaffold } from "../src/new.ts";
import { validateManifest } from "../src/manifest.ts";
import { loadConfig } from "../src/repo.ts";
import { list } from "../src/list.ts";
import { makeRepo, read } from "./fixture.ts";

describe("new", () => {
  it("scaffolds an extension that passes check", () => {
    const root = makeRepo();
    const r = scaffold(root, "demo_app", "Demo App");
    expect(r.files).toContain("apps/server/ext/demo_app/demo_app.go");
    expect(r.files).toContain("packages/ext-demo_app/src/demo-app-editor.tsx");
    expect(check(root)).toEqual([]);

    const m = JSON.parse(read(root, "apps/server/ext/demo_app/pp-app.json"));
    expect(validateManifest(m)).toEqual([]);
    expect(m.name).toBe("Demo App");
    expect(m.server.package).toBe("github.com/nhatbui/pp-hub/apps/server/ext/demo_app");
    expect(m.nodeKinds).toEqual([{ kind: "demo_app.item" }]);
    expect(read(root, "apps/server/ext/demo_app/migrations/0001_init.sql")).toContain(
      "REFERENCES public.nodes(id) ON DELETE CASCADE",
    );
    expect(read(root, "apps/server/ext/demo_app/demo_app.go")).toContain("package demo_app");
    expect(read(root, "packages/ext-demo_app/src/demo-app-editor.tsx")).toContain(
      "export default function DemoAppEditor",
    );
    expect(read(root, "packages/ext-demo_app/src/index.ts")).toContain(
      'import("./demo-app-editor")',
    );
    expect(read(root, "apps/server/cmd/server/extensions_gen.go")).toContain("demo_app.New()");
    expect(read(root, "apps/web/src/ext/registry.gen.ts")).toContain("demo_appExt");
    expect(
      JSON.parse(read(root, "apps/web/package.json")).dependencies["@pp-hub/ext-demo_app"],
    ).toBe("workspace:*");
    expect(loadConfig(root).extensions.map((e) => e.id)).toEqual([
      "kanban",
      "checklist",
      "demo_app",
    ]);
    expect(list(root)).toContain("demo_app");
  });

  it("leaves no unreplaced placeholders", () => {
    const root = makeRepo();
    for (const f of scaffold(root, "demo").files) expect(read(root, f), f).not.toMatch(/\{\{/);
  });

  it("defaults the name from the id", () => {
    const root = makeRepo();
    scaffold(root, "team_notes");
    expect(JSON.parse(read(root, "apps/server/ext/team_notes/pp-app.json")).name).toBe(
      "Team Notes",
    );
  });

  it("refuses an existing or invalid id and changes nothing", () => {
    const root = makeRepo();
    const before = read(root, "pp-apps.json");
    expect(() => scaffold(root, "kanban")).toThrow(/already in pp-apps.json/);
    expect(() => scaffold(root, "Bad-Id")).toThrow(/must match/);
    expect(() => scaffold(root, "type")).toThrow(/Go keyword/);
    expect(() => scaffold(root, "demo", 'Qu"ote')).toThrow(/plain text/);
    fs.mkdirSync(path.join(root, "packages/ext-taken"), { recursive: true });
    expect(() => scaffold(root, "taken")).toThrow(/already exists/);
    expect(read(root, "pp-apps.json")).toBe(before);
  });

  it("remove unregisters an extension but keeps its files", () => {
    const root = makeRepo();
    scaffold(root, "demo");
    remove(root, "demo");
    expect(check(root)).toEqual([]);
    expect(loadConfig(root).extensions.map((e) => e.id)).toEqual(["kanban", "checklist"]);
    expect(fs.existsSync(path.join(root, "apps/server/ext/demo/pp-app.json"))).toBe(true);
    expect(read(root, "apps/server/cmd/server/extensions_gen.go")).not.toContain("demo");
    expect(JSON.parse(read(root, "apps/web/package.json")).dependencies).not.toHaveProperty(
      "@pp-hub/ext-demo",
    );
    expect(() => remove(root, "demo")).toThrow(/not in pp-apps.json/);
  });
});
