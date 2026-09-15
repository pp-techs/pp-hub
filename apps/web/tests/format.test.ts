import { expect, test } from "vite-plus/test";
import { fmtRelative, fmtSize } from "../src/lib/api/utils.ts";

/**
 * The trash and the storage meter both report whole-drive figures, so the
 * scale has to keep climbing past MB — a 1.4 GB archive read as "1433.6 MB"
 * before these units existed.
 */
test("fmtSize climbs past megabytes", () => {
  expect(fmtSize(0)).toBe("0 B");
  expect(fmtSize(512)).toBe("512 B");
  expect(fmtSize(1024)).toBe("1.0 KB");
  expect(fmtSize(1024 ** 2)).toBe("1.0 MB");
  expect(fmtSize(1024 ** 3)).toBe("1.0 GB");
  expect(fmtSize(1.4 * 1024 ** 3)).toBe("1.4 GB");
  expect(fmtSize(2 * 1024 ** 4)).toBe("2.0 TB");
});

test("fmtRelative reads as elapsed time inside a week", () => {
  const ago = (seconds: number) => new Date(Date.now() - seconds * 1000).toISOString();

  expect(fmtRelative(undefined)).toBe("—");
  expect(fmtRelative("not a date")).toBe("—");
  expect(fmtRelative(ago(5))).toBe("just now");
  expect(fmtRelative(ago(120))).toMatch(/2 minutes ago/);
  expect(fmtRelative(ago(3 * 3600))).toMatch(/3 hours ago/);
  expect(fmtRelative(ago(2 * 86400))).toMatch(/2 days ago/);
});

/** Past a week a date is easier to read than a count of days. */
test("fmtRelative falls back to a date beyond a week", () => {
  const old = new Date(Date.now() - 30 * 86400 * 1000).toISOString();
  expect(fmtRelative(old)).not.toMatch(/ago/);
});
