import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { strFromU8, unzipSync } from "fflate";
import { describe, expect, it } from "vite-plus/test";
import { init, idFromDir, SDK_DIR } from "../src/init.ts";
import { pack } from "../src/pack.ts";
import { validatePackagedManifest } from "../src/package-manifest.ts";
import { validate } from "./json-schema.ts";
import { json, put } from "./fixture.ts";

const repo = path.resolve(import.meta.dirname, "../../..");
const schema = JSON.parse(
  fs.readFileSync(path.join(repo, "packages/app-sdk/pp-app.schema.json"), "utf8"),
);

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), "pp-app-"));

function valid(): Record<string, unknown> {
  return {
    id: "tasks",
    name: "Tasks",
    version: "1.0.0",
    description: "A shared task list",
    author: "Acme",
    license: "MIT",
    homepage: "https://acme.example",
    engines: { sdk: 1 },
    icon: "icon.svg",
    nodeKinds: [
      {
        kind: "tasks.list",
        label: "Task list",
        newLabel: "New task list",
        defaultName: "Untitled tasks",
        newHint: "A list",
        icon: "icons/list.svg",
        seed: [{ collection: "items", data: { text: "First task", done: false } }],
      },
    ],
    web: { entry: "index.html", connect: ["https://api.example.com"] },
    backend: { url: "https://tasks.example.com/pp" },
    notifications: { assigned: "{actor} assigned you a task: {excerpt}" },
    translations: { vi: { Tasks: "Công việc" } },
  };
}

type Mutate = (m: Record<string, any>) => void;
/** Rejections both the validator and the JSON Schema can see. */
const bad: [string, Mutate][] = [
  ["unknown top-level field", (m) => (m.extra = 1)],
  ["unknown kind field", (m) => (m.nodeKinds[0].extra = 1)],
  ["unknown web field", (m) => (m.web.base = "./")],
  ["missing description", (m) => delete m.description],
  ["bad id", (m) => (m.id = "Tasks")],
  ["name too long", (m) => (m.name = "x".repeat(61))],
  ["blank author", (m) => (m.author = " ")],
  ["bad semver", (m) => (m.version = "1.0")],
  ["wrong sdk", (m) => (m.engines = { sdk: 2 })],
  ["no kinds", (m) => (m.nodeKinds = [])],
  [
    "nine kinds",
    (m) =>
      (m.nodeKinds = Array.from({ length: 9 }, (_, i) => ({ kind: `tasks.k${i}`, label: "K" }))),
  ],
  ["missing label", (m) => delete m.nodeKinds[0].label],
  ["kind with a bad name", (m) => (m.nodeKinds[0].kind = "tasks.List")],
  ["icon not svg/png", (m) => (m.icon = "icon.gif")],
  ["entry not html", (m) => (m.web.entry = "index.htm")],
  ["absolute entry", (m) => (m.web.entry = "/index.html")],
  ["connect with a path", (m) => (m.web.connect = ["https://api.example.com/v1"])],
  ["http connect", (m) => (m.web.connect = ["http://api.example.com"])],
  ["seed data not an object", (m) => (m.nodeKinds[0].seed = [{ collection: "items", data: [1] }])],
  ["bad seed collection", (m) => (m.nodeKinds[0].seed = [{ collection: "Items", data: {} }])],
  ["bad verb", (m) => (m.notifications = { Assigned: "x" })],
  ["blank template", (m) => (m.notifications = { assigned: " " })],
  ["bad locale", (m) => (m.translations = { vietnamese: {} })],
  ["non-string translation", (m) => (m.translations = { vi: { Tasks: 1 } })],
  ["built-in manifest form", (m) => (m.web = { package: "@acme/tasks" })],
  ["server field", (m) => (m.server = { package: "example.com/tasks" })],
];
/** Rejections only the validator sees (the schema cannot express them). */
const validatorOnly: [string, Mutate, RegExp][] = [
  [
    "kind with another app's prefix",
    (m) => (m.nodeKinds[0].kind = "notes.list"),
    /must be "tasks\."/,
  ],
  ["duplicate kinds", (m) => m.nodeKinds.push({ ...m.nodeKinds[0] }), /listed twice/],
  ["backend url with a query", (m) => (m.backend = { url: "https://x.example/pp?a=1" }), /query/],
  ["ftp homepage", (m) => (m.homepage = "ftp://x.example"), /http\(s\) URL/],
  [
    "large seed",
    (m) => (m.nodeKinds[0].seed = [{ collection: "items", data: { t: "x".repeat(70000) } }]),
    /64 KiB/,
  ],
  [
    "connect origin with a trailing slash",
    (m) => (m.web.connect = ["https://api.example.com/"]),
    /bare https origin/,
  ],
];

describe("validatePackagedManifest", () => {
  it("accepts the contract's example and a minimal manifest", () => {
    expect(validatePackagedManifest(valid()).problems).toEqual([]);
    const m = valid();
    for (const k of ["license", "homepage", "icon", "backend", "notifications", "translations"])
      delete m[k];
    m.nodeKinds = [{ kind: "tasks.list", label: "Task list", newLabel: "" }];
    m.web = { entry: "dist/index.html" };
    expect(validatePackagedManifest(m).problems).toEqual([]);
    expect(validate(schema, m)).toEqual([]);
  });

  it.each(bad)("rejects %s", (_, mutate) => {
    const m = valid();
    mutate(m);
    expect(validatePackagedManifest(m).problems.length).toBeGreaterThan(0);
  });

  it.each(validatorOnly)("rejects %s", (_, mutate, message) => {
    const m = valid();
    mutate(m);
    expect(validatePackagedManifest(m).problems.join("\n")).toMatch(message);
  });

  it("names the unknown field", () => {
    const m = valid();
    m.extra = 1;
    expect(validatePackagedManifest(m).problems).toEqual(['pp-app.json: unknown field "extra"']);
  });
});

describe("app-sdk pp-app.schema.json agrees with the validator", () => {
  it("is a draft 2020-12 schema that accepts the full example", () => {
    expect(schema.$schema).toBe("https://json-schema.org/draft/2020-12/schema");
    expect(validate(schema, valid())).toEqual([]);
  });

  it.each(bad)("rejects %s", (name, mutate) => {
    const m = valid();
    mutate(m);
    expect(validate(schema, m).length, name).toBeGreaterThan(0);
  });
});

describe("init and pack", () => {
  it("scaffolds an app that packs into a valid zip", () => {
    const dir = path.join(tmp(), "my-board");
    const r = init(dir, { name: "My Board" });
    expect(r.id).toBe("my_board");
    for (const f of r.files.filter((f) => !f.startsWith("app-sdk.")))
      expect(fs.readFileSync(path.join(dir, f), "utf8"), f).not.toMatch(/\{\{/);
    expect(fs.readFileSync(path.join(dir, "app-sdk.js"), "utf8")).toBe(
      fs.readFileSync(path.join(SDK_DIR, "index.js"), "utf8"),
    );
    const m = JSON.parse(fs.readFileSync(path.join(dir, "pp-app.json"), "utf8"));
    expect(validatePackagedManifest(m).problems).toEqual([]);
    expect(validate(schema, m)).toEqual([]);
    expect(m.nodeKinds[0].kind).toBe("my_board.doc");
    expect(m.name).toBe("My Board");

    // Noise pack must leave out.
    put(dir, ".DS_Store", "x");
    put(dir, ".git/config", "x");
    put(dir, "node_modules/x/index.js", "x");
    put(dir, "old.zip", "x");

    const out = pack(dir);
    expect(out.file).toBe(path.join(path.dirname(dir), "my_board-0.1.0.zip"));
    const entries = unzipSync(fs.readFileSync(out.file));
    expect(Object.keys(entries).sort()).toEqual([...r.files].sort());
    expect(out.files).toEqual(Object.keys(entries).sort());
    expect(JSON.parse(strFromU8(entries["pp-app.json"]!)).id).toBe("my_board");
    expect(out.bytes).toBe(fs.statSync(out.file).size);
  });

  it("writes to -o, inside the app directory too, without packing itself", () => {
    const dir = path.join(tmp(), "notes");
    init(dir);
    const first = pack(dir, path.join(dir, "out", "notes.bin"));
    const again = pack(dir, path.join(dir, "out", "notes.bin"));
    expect(again.files).toEqual(first.files);
    expect(again.files).not.toContain("out/notes.bin");
  });

  it("packs only the entry's directory when the entry is nested", () => {
    const dir = tmp();
    put(dir, "pp-app.json", json({ ...valid(), web: { entry: "dist/index.html" } }));
    put(dir, "icon.svg", "<svg/>");
    put(dir, "icons/list.svg", "<svg/>");
    put(dir, "icons/unused.svg", "<svg/>");
    put(dir, "dist/index.html", '<script type="module" src="./assets/a.js"></script>');
    put(dir, "dist/assets/a.js", "");
    put(dir, "src/main.ts", "");
    put(dir, "package.json", "{}");
    expect(pack(dir, path.join(tmp(), "t.zip")).files).toEqual([
      "dist/assets/a.js",
      "dist/index.html",
      "icon.svg",
      "icons/list.svg",
      "pp-app.json",
    ]);
  });

  it("rejects root-absolute asset references, pointing at Vite's base", () => {
    const dir = tmp();
    put(dir, "pp-app.json", json({ ...valid(), web: { entry: "index.html" } }));
    put(dir, "icon.svg", "<svg/>");
    put(dir, "icons/list.svg", "<svg/>");
    put(
      dir,
      "index.html",
      '<link href="//cdn.example/x.css">\n<script src="/assets/a.js"></script>',
    );
    expect(() => pack(dir, path.join(tmp(), "t.zip"))).toThrow(/index\.html:2.*base: "\.\/"/);
  });

  it("rejects missing referenced files and invalid manifests", () => {
    const dir = tmp();
    put(dir, "pp-app.json", json(valid()));
    put(dir, "index.html", "");
    expect(() => pack(dir)).toThrow(/icon\.svg: referenced in pp-app.json but not found/);
    put(dir, "pp-app.json", json({ ...valid(), engines: { sdk: 2 } }));
    expect(() => pack(dir)).toThrow(/engines\.sdk: must be 1/);
  });

  it("init refuses a non-empty directory and a bad id", () => {
    const dir = tmp();
    put(dir, "x.txt", "");
    expect(() => init(dir, { id: "ok" })).toThrow(/not an empty directory/);
    expect(() => init(path.join(tmp(), "x"), { id: "Bad" })).toThrow(/must match/);
    expect(() => init(path.join(tmp(), "123"))).toThrow(/pass --id/);
    expect(idFromDir("/a/My Cool-App!")).toBe("my_cool_app");
  });
});
