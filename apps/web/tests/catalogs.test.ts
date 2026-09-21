import { describe, expect, it } from "vite-plus/test";
import { compileMessage } from "@lingui/message-utils/compileMessage";

/**
 * Catalogs are read through Vite's raw glob rather than `node:fs`: the app's
 * tsconfig deliberately does not pull in Node types, and `?raw` also skips
 * `@lingui/vite-plugin`, which would otherwise hand back a compiled message
 * tree instead of the `.po` text this is here to inspect.
 */
const CATALOGS = import.meta.glob("../src/locales/*/messages.po", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const SOURCE_LOCALE = "en";

interface Entry {
  msgid: string;
  msgstr: string;
}

/**
 * A minimal gettext reader. Enough for Lingui's output shape — `msgid` and
 * `msgstr`, each possibly continued over several quoted lines — and
 * deliberately not a general PO parser: the point is to check the catalogs,
 * not to depend on a library that might normalise away the very defects we
 * are looking for.
 */
function parsePo(text: string): Entry[] {
  const entries: Entry[] = [];
  let field: "msgid" | "msgstr" | null = null;
  let msgid = "";
  let msgstr = "";

  const flush = () => {
    // The header entry has an empty msgid; it carries metadata, not a message.
    if (msgid !== "") entries.push({ msgid, msgstr });
    msgid = "";
    msgstr = "";
    field = null;
  };

  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line === "") {
      flush();
      continue;
    }
    if (line.startsWith("#")) continue;

    const quoted = (s: string) => JSON.parse(s.slice(s.indexOf('"'))) as string;
    if (line.startsWith("msgid ")) {
      flush();
      field = "msgid";
      msgid = quoted(line);
    } else if (line.startsWith("msgstr ")) {
      field = "msgstr";
      msgstr = quoted(line);
    } else if (line.startsWith('"') && field) {
      if (field === "msgid") msgid += quoted(line);
      else msgstr += quoted(line);
    }
  }
  flush();
  return entries;
}

/**
 * The ICU arguments and rich-text tags a message actually references.
 *
 * This walks the tree `compileMessage` produces rather than scanning for
 * `{...}`, because the two are not the same thing: in
 * `{kind, select, dir {folder} other {file}}` the braces around `folder` and
 * `file` delimit sub-messages, and a regex reads them as arguments named
 * `folder` and `file`. A translation that correctly renders those words in
 * its own language would then look like it had dropped two placeholders.
 *
 * Arguments are what the code actually passes, so an extra one renders as
 * literal braces and a missing one silently drops the data the sentence was
 * about. Sub-message text is prose and must be free to change.
 */
function args(message: string): Set<string> {
  const found = new Set<string>();

  // `compileMessage` yields a list of parts: a string part is literal text, an
  // array part is an argument `[name, type?, cases?]`. A plural/select case
  // maps to another list of parts, so sub-messages recurse. "Cancel" compiles
  // to ["Cancel"] — one literal part, no arguments — which is exactly why the
  // top level cannot itself be read as an argument.
  const collect = (parts: unknown) => {
    if (!Array.isArray(parts)) return;
    for (const part of parts) {
      if (!Array.isArray(part)) continue;
      const [name, , cases] = part as [unknown, unknown, unknown];
      if (typeof name === "string") found.add(name);
      if (cases && typeof cases === "object" && !Array.isArray(cases)) {
        Object.values(cases).forEach(collect);
      }
    }
  };
  collect(compileMessage(message));

  for (const m of message.matchAll(/<(\d+)>/g)) found.add(`<${m[1]}>`);
  return found;
}

/** Locale tag of a globbed path, e.g. `../src/locales/vi/messages.po` -> `vi`. */
function localeOf(path: string): string {
  return path.split("/").at(-2)!;
}

function locales(): string[] {
  return Object.keys(CATALOGS).map(localeOf);
}

function load(locale: string): Entry[] {
  const path = Object.keys(CATALOGS).find((p) => localeOf(p) === locale);
  if (!path) throw new Error(`no catalog for ${locale}`);
  return parsePo(CATALOGS[path]!);
}

/**
 * Catalogs are edited by translators in Weblate and, for a self-hoster, may
 * be edited by hand. Weblate runs its own ICU check, but a catalog that
 * arrived any other way has had none — so the invariant is asserted here,
 * where every path into the repository has to pass through it.
 */
describe("message catalogs", () => {
  const source = load(SOURCE_LOCALE);
  const sourceById = new Map(source.map((e) => [e.msgid, e]));
  const translations = locales().filter((l) => l !== SOURCE_LOCALE);

  it("ships at least one translation beside the source", () => {
    expect(translations.length).toBeGreaterThan(0);
  });

  for (const locale of translations) {
    describe(locale, () => {
      const entries = load(locale);

      it("carries no message the source does not define", () => {
        const orphans = entries.filter((e) => !sourceById.has(e.msgid)).map((e) => e.msgid);
        expect(orphans).toEqual([]);
      });

      it("parses as ICU", () => {
        // `compileMessage` is the same parser the build uses, so a message
        // that fails here would have failed the build — except that a
        // catalog can arrive from Weblate or a hand edit without ever being
        // built, which is the case this exists for.
        const broken: string[] = [];
        for (const entry of entries) {
          if (entry.msgstr === "") continue;
          try {
            compileMessage(entry.msgstr);
          } catch (err) {
            broken.push(`${entry.msgid}: ${err instanceof Error ? err.message : String(err)}`);
          }
        }
        expect(broken).toEqual([]);
      });

      it("passes every argument its source does", () => {
        const broken: string[] = [];
        for (const entry of entries) {
          // An untranslated entry is a normal state, not a defect: it falls
          // back to the source string at runtime.
          if (entry.msgstr === "") continue;
          const want = args(entry.msgid);
          const got = args(entry.msgstr);
          const missing = [...want].filter((p) => !got.has(p));
          const extra = [...got].filter((p) => !want.has(p));
          if (missing.length || extra.length) {
            broken.push(
              `${entry.msgid} (missing: ${missing.join(", ")}, extra: ${extra.join(", ")})`,
            );
          }
        }
        expect(broken).toEqual([]);
      });
    });
  }
});
