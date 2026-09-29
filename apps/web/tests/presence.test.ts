import { expect, test } from "vite-plus/test";
import { presenceColor, presenceInitials } from "../src/lib/collab/presence.ts";

test("initials take the first and last word of a display name", () => {
  expect(presenceInitials("Bùi Đức Nhật")).toBe("BN");
  expect(presenceInitials("  ada   lovelace ")).toBe("AL");
  expect(presenceInitials("Ada")).toBe("A");
});

test("initials fall back to an email's local part, split on its punctuation", () => {
  expect(presenceInitials("nhat.bui@example.com")).toBe("NB");
  expect(presenceInitials("ops+alerts@example.com")).toBe("OA");
  expect(presenceInitials("x@example.com")).toBe("X");
});

test("initials never render empty", () => {
  expect(presenceInitials("")).toBe("?");
  expect(presenceInitials("   ")).toBe("?");
});

test("a user keeps one caret colour, a valid #RRGGBB, and users spread across the palette", () => {
  const ids = Array.from(
    { length: 64 },
    (_, i) => `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`,
  );
  for (const id of ids) {
    expect(presenceColor(id)).toBe(presenceColor(id));
    expect(presenceColor(id)).toMatch(/^#[0-9A-F]{6}$/);
  }
  expect(new Set(ids.map(presenceColor)).size).toBeGreaterThan(6);
});
