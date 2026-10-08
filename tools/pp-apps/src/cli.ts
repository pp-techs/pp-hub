#!/usr/bin/env bun
import path from "node:path";
import { fileURLToPath } from "node:url";
import { add } from "./add.ts";
import { check } from "./check.ts";
import { gen, resolveOrThrow, sync } from "./gen.ts";
import { list } from "./list.ts";
import { scaffold } from "./new.ts";
import { loadConfig } from "./repo.ts";
import { remove } from "./remove.ts";

const USAGE = `pp-apps: wire pp-hub extensions (docs/architecture/extensions.md)

  gen                              write the generated registry files and sync manifest copies
  sync                             copy each in-repo pp-app.json into its web package
  check                            verify the tree is consistent; exit 1 with the problems
  list                             show the registered extensions
  new <id> [--name "Title"]        scaffold an in-repo extension
  add <package@version> --go <module@version>
                                   install an external extension
  remove <id>                      unregister an extension (its files stay)
`;

/** The repo root: tools/pp-apps/src/../../.. */
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

function flag(args: string[], name: string): string | undefined {
  const i = args.indexOf(name);
  if (i < 0) return undefined;
  const v = args[i + 1];
  if (v === undefined || v.startsWith("--")) throw new Error(`${name} needs a value`);
  args.splice(i, 2);
  return v;
}

function main(argv: string[]): number {
  const [cmd, ...rest] = argv;
  switch (cmd) {
    case "gen": {
      const changed = gen(ROOT);
      console.log(
        changed.length
          ? `updated:\n${changed.map((f) => `  ${f}`).join("\n")}`
          : "already up to date",
      );
      return 0;
    }
    case "sync": {
      const changed = sync(ROOT, resolveOrThrow(ROOT, loadConfig(ROOT)));
      console.log(
        changed.length ? `updated:\n${changed.map((f) => `  ${f}`).join("\n")}` : "already in sync",
      );
      return 0;
    }
    case "check": {
      const problems = check(ROOT);
      if (problems.length) {
        console.error(
          `pp-apps check found ${problems.length} problem(s):\n${problems.map((p) => `  - ${p}`).join("\n")}`,
        );
        return 1;
      }
      console.log("pp-apps check: ok");
      return 0;
    }
    case "list":
      console.log(list(ROOT));
      return 0;
    case "new": {
      const name = flag(rest, "--name");
      const [id, ...extra] = rest;
      if (!id || extra.length) throw new Error('usage: new <id> [--name "Title"]');
      const r = scaffold(ROOT, id, name);
      console.log(
        `created:\n${r.files.map((f) => `  ${f}`).join("\n")}\n\nnext:\n${r.nextSteps.map((s) => `  ${s}`).join("\n")}`,
      );
      return 0;
    }
    case "add": {
      const go = flag(rest, "--go");
      const [pkg, ...extra] = rest;
      if (!pkg || !go || extra.length)
        throw new Error("usage: add <package@version> --go <module@version>");
      const m = add(ROOT, pkg, go);
      console.log(`added ${m.id} ${m.version}`);
      return 0;
    }
    case "remove": {
      const [id, ...extra] = rest;
      if (!id || extra.length) throw new Error("usage: remove <id>");
      remove(ROOT, id);
      console.log(`removed ${id} from pp-apps.json; its files were left in place`);
      return 0;
    }
    default:
      console.error(USAGE);
      return cmd === undefined || cmd === "help" || cmd === "--help" ? 0 : 1;
  }
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  console.error(`pp-apps: ${(e as Error).message}`);
  process.exitCode = 1;
}
