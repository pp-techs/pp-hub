/* eslint-disable lingui/no-unlocalized-strings -- error reasons, file names and MIME types; the dialog words them */
import { zip, type Zippable } from "fflate";

// An app uploaded as a folder (M51) is zipped here, in the browser, so the
// server reads one package format. The server applies the same skips and
// limits again; these only spare an upload that would be refused.

/** The server's limits (apppkg): files, and bytes before compression. */
export const MAX_APP_FILES = 2000;
export const MAX_APP_BYTES = 50 << 20;
/** The largest upload, compressed. */
export const MAX_APP_UPLOAD = 20 << 20;

export class AppPackageError extends Error {
  readonly reason: "empty" | "too-many-files" | "too-large";
  constructor(reason: AppPackageError["reason"]) {
    super(reason);
    this.reason = reason;
  }
}

/** Whether a path from a picked folder belongs in the package: no dot-files, no dependencies. */
function kept(path: string): boolean {
  return path
    .split("/")
    .every((segment) => segment !== "" && !segment.startsWith(".") && segment !== "node_modules");
}

/**
 * Zips the files of a picked folder under their paths relative to it
 * (`webkitRelativePath`, which starts with the folder's own name — the server
 * strips that one shared directory).
 */
export async function zipFolder(files: readonly File[]): Promise<Blob> {
  const picked = files.filter((f) => kept(f.webkitRelativePath || f.name));
  if (picked.length === 0) throw new AppPackageError("empty");
  if (picked.length > MAX_APP_FILES) throw new AppPackageError("too-many-files");
  if (picked.reduce((n, f) => n + f.size, 0) > MAX_APP_BYTES) {
    throw new AppPackageError("too-large");
  }
  const tree: Zippable = {};
  for (const f of picked) {
    tree[f.webkitRelativePath || f.name] = new Uint8Array(await f.arrayBuffer());
  }
  const bytes = await new Promise<Uint8Array<ArrayBuffer>>((resolve, reject) =>
    zip(tree, { level: 6 }, (err, data) =>
      err ? reject(err) : resolve(data as Uint8Array<ArrayBuffer>),
    ),
  );
  if (bytes.byteLength > MAX_APP_UPLOAD) throw new AppPackageError("too-large");
  return new Blob([bytes], { type: "application/zip" });
}
