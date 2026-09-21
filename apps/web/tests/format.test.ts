import { expect, test } from "vite-plus/test";
import { fmtRelative, fmtSize } from "../src/lib/api/utils.ts";

/**
 * Every case pins a locale explicitly. These helpers used to pass `undefined`
 * to `Intl`, which means "whatever the host is set to" — so the assertions
 * below were really assertions about the machine running them, and would have
 * drifted the moment CI ran somewhere else.
 */

/**
 * The trash and the storage meter both report whole-drive figures, so the
 * scale has to keep climbing past MB — a 1.4 GB archive read as "1433.6 MB"
 * before these units existed.
 */
test("fmtSize climbs past megabytes", () => {
  expect(fmtSize(0, "en")).toBe("0 B");
  expect(fmtSize(512, "en")).toBe("512 B");
  expect(fmtSize(1024, "en")).toBe("1.0 KB");
  expect(fmtSize(1024 ** 2, "en")).toBe("1.0 MB");
  expect(fmtSize(1024 ** 3, "en")).toBe("1.0 GB");
  expect(fmtSize(1.4 * 1024 ** 3, "en")).toBe("1.4 GB");
  expect(fmtSize(2 * 1024 ** 4, "en")).toBe("2.0 TB");
});

/**
 * Vietnamese writes the decimal separator as a comma. A size formatted with
 * the host's locale instead of the reader's is the kind of defect that never
 * shows up in English and is obvious to everyone else.
 */
test("fmtSize follows the locale's decimal separator", () => {
  expect(fmtSize(1.4 * 1024 ** 3, "vi")).toBe("1,4 GB");
});

test("fmtRelative reads as elapsed time inside a week", () => {
  const ago = (seconds: number) => new Date(Date.now() - seconds * 1000).toISOString();

  expect(fmtRelative(undefined, "en")).toBe("—");
  expect(fmtRelative("not a date", "en")).toBe("—");
  expect(fmtRelative(ago(5), "en")).toBe("now");
  expect(fmtRelative(ago(120), "en")).toMatch(/2 minutes ago/);
  expect(fmtRelative(ago(3 * 3600), "en")).toMatch(/3 hours ago/);
  expect(fmtRelative(ago(2 * 86400), "en")).toMatch(/2 days ago/);
});

/**
 * The whole point of threading the locale through: the same instant has to
 * read differently to a Vietnamese reader. Asserting "not the English text"
 * rather than an exact Vietnamese phrase keeps this from breaking when ICU
 * data is updated.
 */
test("fmtRelative follows the active locale", () => {
  const ago = new Date(Date.now() - 120 * 1000).toISOString();
  const en = fmtRelative(ago, "en");
  const vi = fmtRelative(ago, "vi");
  expect(vi).not.toBe(en);
  expect(vi).not.toMatch(/ago/);
});

/** Past a week a date is easier to read than a count of days. */
test("fmtRelative falls back to a date beyond a week", () => {
  const old = new Date(Date.now() - 30 * 86400 * 1000).toISOString();
  expect(fmtRelative(old, "en")).not.toMatch(/ago/);
});
