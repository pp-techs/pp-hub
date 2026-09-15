import { expect, test } from "vite-plus/test";
import { collectInputFiles } from "../src/lib/drop-items.ts";

/**
 * A folder picked with <input webkitdirectory> reports each file's path
 * relative to the chosen folder, name included. The destination folder is
 * everything but that last segment — keeping it would create a directory named
 * after the file.
 */
function pick(relativePath: string): File {
  const file = new File(["x"], relativePath.split("/").pop() ?? relativePath);
  Object.defineProperty(file, "webkitRelativePath", { value: relativePath });
  return file;
}

function asList(files: File[]): FileList {
  return Object.assign(files, { item: (i: number) => files[i] ?? null }) as unknown as FileList;
}

test("collectInputFiles strips the file name from its folder path", () => {
  const items = collectInputFiles(
    asList([
      pick("Invoices/Q1/jan.pdf"),
      pick("Invoices/readme.txt"),
      pick("Invoices/Q1/Scans/scan.png"),
    ]),
  );
  expect(items.map((i) => i.path)).toEqual(["Invoices/Q1", "Invoices", "Invoices/Q1/Scans"]);
});

test("collectInputFiles leaves plainly selected files at the destination", () => {
  const items = collectInputFiles(asList([new File(["x"], "budget.csv")]));
  expect(items).toHaveLength(1);
  expect(items[0].path).toBe("");
});

test("collectInputFiles tolerates an empty picker", () => {
  expect(collectInputFiles(null)).toEqual([]);
});
