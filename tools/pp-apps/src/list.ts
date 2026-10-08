import { loadConfig, resolveAll } from "./repo.ts";

/** The registered extensions as an aligned text table. */
export function list(root: string): string {
  const { resolved, problems } = resolveAll(root, loadConfig(root));
  if (problems.length) throw new Error(problems.join("\n"));
  const rows = [
    ["ID", "NAME", "VERSION", "KINDS", "SOURCE"],
    ...resolved.map((e) => [
      e.manifest.id,
      e.manifest.name,
      e.manifest.version,
      e.manifest.nodeKinds.map((k) => k.kind).join(", "),
      e.external ? "external" : "in-repo",
    ]),
  ];
  const widths = rows[0]!.map((_, c) => Math.max(...rows.map((r) => r[c]!.length)));
  return rows
    .map((r) =>
      r
        .map((cell, c) => cell.padEnd(widths[c]!))
        .join("  ")
        .trimEnd(),
    )
    .join("\n");
}
