import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { validateManifest } from "../src/manifest.ts";
import { validate } from "./json-schema.ts";
import { manifestFor } from "./fixture.ts";

const repo = path.resolve(import.meta.dirname, "../../..");
const schema = JSON.parse(
  fs.readFileSync(path.join(repo, "packages/ext-sdk/pp-app.schema.json"), "utf8"),
);

const bad: [string, (m: Record<string, unknown>) => void][] = [
  ["missing id", (m) => delete m.id],
  ["uppercase id", (m) => (m.id = "Kanban")],
  ["id too long", (m) => (m.id = "a".repeat(33))],
  ["id with a dash", (m) => (m.id = "my-app")],
  ["Go keyword id", (m) => (m.id = "type")],
  ["blank name", (m) => (m.name = " ")],
  ["bad semver", (m) => (m.version = "1.0")],
  ["leading-zero semver", (m) => (m.version = "01.0.0")],
  ["missing description", (m) => delete m.description],
  ["missing author", (m) => delete m.author],
  ["non-string license", (m) => (m.license = 3)],
  ["fractional sdk", (m) => (m.engines = { sdk: 1.5 })],
  ["sdk zero", (m) => (m.engines = { sdk: 0 })],
  ["string sdk", (m) => (m.engines = { sdk: "1" })],
  ["extra engines field", (m) => (m.engines = { sdk: 1, node: 22 })],
  ["unknown top-level field", (m) => (m.extra = true)],
  ["kind not a string", (m) => (m.nodeKinds = [{ kind: 3 }])],
  ["kind without dot", (m) => (m.nodeKinds = [{ kind: "thing" }])],
  ["kind extra field", (m) => (m.nodeKinds = [{ kind: "x.thing", icon: "a" }])],
  ["empty server package", (m) => (m.server = { package: "" })],
  ["server extra field", (m) => (m.server = { package: "a/b", dir: "x" })],
  ["missing web", (m) => delete m.web],
];

describe("validateManifest", () => {
  it("accepts a full manifest and one with the optional fields", () => {
    expect(validateManifest(manifestFor("x"))).toEqual([]);
    expect(
      validateManifest({ ...manifestFor("x"), license: "MIT", homepage: "https://example.com" }),
    ).toEqual([]);
    expect(validateManifest({ ...manifestFor("x"), version: "2.1.0-rc.1+build.5" })).toEqual([]);
  });

  it.each(bad)("rejects %s", (_, mutate) => {
    const m: Record<string, unknown> = { ...manifestFor("x") };
    mutate(m);
    expect(validateManifest(m).length).toBeGreaterThan(0);
  });

  it("requires every kind to be prefixed with the id, and unique", () => {
    const wrong = validateManifest(manifestFor("x", { nodeKinds: [{ kind: "y.thing" }] }));
    expect(wrong.join("\n")).toContain('must start with "x."');
    const twice = validateManifest(
      manifestFor("x", { nodeKinds: [{ kind: "x.a" }, { kind: "x.a" }] }),
    );
    expect(twice.join("\n")).toContain("listed twice");
  });

  it("names the field in each message", () => {
    expect(validateManifest({ ...manifestFor("x"), version: "nope" }, "m.json")[0]).toMatch(
      /^m\.json\.version:/,
    );
  });
});

describe("pp-app.schema.json agrees with the validator", () => {
  it("accepts both built-in manifests", () => {
    for (const id of ["kanban", "checklist"]) {
      const m = JSON.parse(
        fs.readFileSync(path.join(repo, `apps/server/ext/${id}/pp-app.json`), "utf8"),
      );
      expect(validate(schema, m)).toEqual([]);
      expect(validateManifest(m)).toEqual([]);
    }
  });

  it("is a draft 2020-12 schema", () => {
    expect(schema.$schema).toBe("https://json-schema.org/draft/2020-12/schema");
  });

  it.each(bad)("rejects %s like the validator does", (name, mutate) => {
    const m: Record<string, unknown> = { ...manifestFor("x") };
    mutate(m);
    // The two are not expected to match message for message, only verdict for verdict.
    expect(validate(schema, m).length > 0, name).toBe(validateManifest(m).length > 0);
  });

  it("accepts what the validator accepts", () => {
    const m = { ...manifestFor("my_app"), license: "MIT", homepage: "https://example.com" };
    expect(validate(schema, m)).toEqual([]);
    expect(validateManifest(m)).toEqual([]);
  });
});
