#!/usr/bin/env bun
import { init } from "./init.ts";
import { pack } from "./pack.ts";

const USAGE = `pp-apps: write and package pp-hub apps (docs/architecture/extension-authoring.md)

  init <dir> [--id <id>] [--name "Title"]   scaffold an app with no build step into <dir>
  pack <dir> [-o <file.zip>]                validate <dir>/pp-app.json and write the zip to upload
`;

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
    case "init": {
      const id = flag(rest, "--id");
      const name = flag(rest, "--name");
      const [dir, ...extra] = rest;
      if (!dir || extra.length) throw new Error('usage: init <dir> [--id <id>] [--name "Title"]');
      const r = init(dir, { id, name });
      console.log(
        `created ${r.id} in ${r.dir}:\n${r.files.map((f) => `  ${f}`).join("\n")}\n\nnext:\n${r.nextSteps.map((s) => `  ${s}`).join("\n")}`,
      );
      return 0;
    }
    case "pack": {
      const out = flag(rest, "-o");
      const [dir, ...extra] = rest;
      if (!dir || extra.length) throw new Error("usage: pack <dir> [-o <file.zip>]");
      const r = pack(dir, out);
      const size = r.bytes < 1024 ? `${r.bytes} B` : `${(r.bytes / 1024).toFixed(1)} KiB`;
      console.log(`packed ${r.file} (${r.files.length} files, ${size})`);
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
