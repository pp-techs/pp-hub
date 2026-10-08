/// <reference types="node" />
import path from "node:path";
import { expect, test } from "vite-plus/test";
import { check } from "../../../tools/pp-apps/src/check.ts";

const repoRoot = path.resolve(import.meta.dirname, "../../..");

// pp-apps.json, the manifests, the web packages and the two generated registry
// files must agree; `bun run apps gen` repairs most drift.
test("pp-apps.json, manifests and generated registries are consistent", () => {
  expect(check(repoRoot)).toEqual([]);
});
