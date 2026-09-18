import type { UploadItem } from "@/lib/upload-store";

/**
 * Flattens a drop into files plus their folder paths.
 *
 * Dropping a folder only yields its contents through the non-standard
 * `webkitGetAsEntry` API, which every current browser implements and nothing
 * has replaced. Where it is missing the drop degrades to the plain file list,
 * which loses folder structure but still uploads the files.
 */
export async function collectDropItems(dt: DataTransfer): Promise<UploadItem[]> {
  const entries = [...dt.items]
    .filter((item) => item.kind === "file")
    .map((item) => (item.webkitGetAsEntry ? item.webkitGetAsEntry() : null));

  if (entries.every((e) => e === null)) {
    return [...dt.files].map((file) => ({ file, path: "" }));
  }

  const out: UploadItem[] = [];
  await Promise.all(entries.map((entry) => (entry ? walk(entry, "", out) : Promise.resolve())));
  return out;
}

/** Turns a file input's FileList into upload items, honouring webkitRelativePath. */
export function collectInputFiles(list: FileList | null): UploadItem[] {
  return [...(list ?? [])].map((file) => ({
    file,
    // Set by <input webkitdirectory>: "Invoices/Q1/file.pdf". The file's own
    // name is not part of the destination path.
    path: file.webkitRelativePath ? file.webkitRelativePath.split("/").slice(0, -1).join("/") : "",
  }));
}

async function walk(entry: FileSystemEntry, prefix: string, out: UploadItem[]): Promise<void> {
  if (entry.isFile) {
    const file = await new Promise<File | null>((resolve) =>
      (entry as FileSystemFileEntry).file(resolve, () => resolve(null)),
    );
    // macOS folders carry a .DS_Store that nobody means to upload.
    if (file && file.name !== ".DS_Store") out.push({ file, path: prefix });
    return;
  }
  if (!entry.isDirectory) return;

  const path = prefix ? `${prefix}/${entry.name}` : entry.name;
  const children = await readAll((entry as FileSystemDirectoryEntry).createReader());
  // Walk subtrees concurrently but concatenate in entry order so the result
  // matches serial depth-first traversal.
  const subtrees = await Promise.all(
    children.map(async (child) => {
      const items: UploadItem[] = [];
      await walk(child, path, items);
      return items;
    }),
  );
  for (const items of subtrees) out.push(...items);
}

/**
 * readEntries hands back at most 100 entries per call and signals the end with
 * an empty batch, so a large folder has to be drained in a loop.
 */
async function readAll(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  const all: FileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve) =>
      reader.readEntries(resolve, () => resolve([])),
    );
    if (batch.length === 0) return all;
    all.push(...batch);
  }
}
