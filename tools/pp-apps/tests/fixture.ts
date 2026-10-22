import fs from "node:fs";
import path from "node:path";

export function put(root: string, rel: string, text: string) {
  const file = path.join(root, rel);
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, text);
}

export const json = (v: unknown) => `${JSON.stringify(v, null, 2)}\n`;
