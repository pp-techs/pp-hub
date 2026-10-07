/**
 * Version step of the release flow (see CONTRIBUTING.md#releasing). The release workflow runs it
 * to build the "Version Packages" PR; run it locally only to preview that PR.
 *
 * pp-hub ships one product version, not one per workspace package. `@pp-hub/release` exists only
 * to carry it through Changesets, so `changeset version` bumps that package; on top of that this
 * gives the root package the same version, writes the root CHANGELOG section from the changesets
 * it consumes, and refreshes `bun.lock`, which records workspace versions. The package-level
 * `tools/release/CHANGELOG.md` Changesets writes is kept: changesets/action reads it to build the
 * PR description, and fails without it. The root CHANGELOG.md is the one people read.
 */
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";

const CHANGESET_DIR = ".changeset";
const CARRIER = "tools/release";
const ROOT_CHANGELOG = "CHANGELOG.md";
const CHANGELOG_MARKER =
  "<!-- `bun run version-packages` adds each release's section from its changesets; edit it in the Version Packages PR. -->";

function run(command: string, args: string[]): void {
  execFileSync(command, args, { stdio: "inherit" });
}

function readVersion(manifestPath: string): string {
  const { version } = JSON.parse(readFileSync(manifestPath, "utf8")) as { version: string };
  return version;
}

const changesetFiles = readdirSync(CHANGESET_DIR)
  .filter((file) => file.endsWith(".md") && file !== "README.md")
  .sort();
if (changesetFiles.length === 0) {
  throw new Error(`No pending changesets in ${CHANGESET_DIR}/: nothing to version.`);
}
// Read before `changeset version` deletes them.
const notes = changesetFiles.map((file) =>
  readFileSync(`${CHANGESET_DIR}/${file}`, "utf8")
    .replace(/^---\n[\s\S]*?\n---\n/, "")
    .trim(),
);

run("bun", ["x", "changeset", "version"]);

const version = readVersion(`${CARRIER}/package.json`);

const rootManifest = readFileSync("package.json", "utf8");
writeFileSync(
  "package.json",
  rootManifest.replace(/"version": "[^"]+"/, `"version": "${version}"`),
);

const changelog = readFileSync(ROOT_CHANGELOG, "utf8");
if (!changelog.includes(CHANGELOG_MARKER)) {
  throw new Error(`${ROOT_CHANGELOG} lost its marker comment:\n${CHANGELOG_MARKER}`);
}
const date = new Date().toISOString().slice(0, 10);
const section = `## [${version}] — ${date}\n\n${notes.join("\n\n")}`;
writeFileSync(
  ROOT_CHANGELOG,
  changelog.replace(CHANGELOG_MARKER, `${CHANGELOG_MARKER}\n\n${section}`),
);

run("bun", ["install"]);
run("bun", [
  "x",
  "vp",
  "fmt",
  "package.json",
  ROOT_CHANGELOG,
  `${CARRIER}/package.json`,
  `${CARRIER}/CHANGELOG.md`,
]);

console.log(`Versioned the product to ${version}.`);
