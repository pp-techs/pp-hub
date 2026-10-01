import { i18n } from "@lingui/core";
import type { Messages, MessageDescriptor } from "@lingui/core";

/**
 * Locales this build ships. A locale exists here only when
 * `src/locales/<tag>/messages.po` does: the catalog is imported statically
 * below, so an entry without a file is a build error rather than a blank UI.
 *
 * Adding a locale is: translate it in Weblate, merge the `.po`, add the tag
 * here. There is deliberately no runtime catalog fetch — the Vite plugin
 * pre-compiles every catalog, which is why the bundle carries no ICU parser.
 */
export const LOCALES = {
  en: "English",
  vi: "Tiếng Việt",
} as const;

export type Locale = keyof typeof LOCALES;

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_STORAGE_KEY = "pp-hub-locale:v1";

const LOCALE_TAGS = Object.keys(LOCALES) as Locale[];

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && LOCALE_TAGS.includes(value as Locale);
}

/**
 * Narrow a BCP-47 tag to a locale we ship: `vi-VN` and `vi` both mean `vi`.
 * A tag with no catalog returns null so the caller can fall through to the
 * next source rather than activating something that would render ids.
 */
export function matchLocale(tag: string | null | undefined): Locale | null {
  if (!tag) return null;
  const lower = tag.toLowerCase();
  const base = lower.split("-")[0]!;
  return LOCALE_TAGS.find((l) => l === lower || l === base) ?? null;
}

function readStored(): Locale | null {
  try {
    return matchLocale(localStorage.getItem(LOCALE_STORAGE_KEY));
  } catch {
    return null;
  }
}

export function storeLocale(locale: Locale) {
  try {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // Private mode or a blocked origin. The server preference still holds it
    // for the next sign-in; only the pre-auth guess is lost.
  }
}

/**
 * The locale to start in, before the server has said anything.
 *
 * `localStorage` first and `navigator.languages` second: the stored value is
 * this device's echo of the account preference, written every time the
 * account preference is applied, so a returning user paints in their own
 * language on the very first frame instead of flashing English. `/login`,
 * `/s/{token}` and `/invite/{token}` have no account at all and live entirely
 * on this chain.
 */
export function detectLocale(): Locale {
  const stored = readStored();
  if (stored) return stored;
  for (const tag of navigator.languages ?? [navigator.language]) {
    const match = matchLocale(tag);
    if (match) return match;
  }
  return DEFAULT_LOCALE;
}

const catalogs: Record<Locale, () => Promise<{ messages: Messages }>> = {
  en: () => import("@/locales/en/messages.po"),
  vi: () => import("@/locales/vi/messages.po"),
};

/**
 * The product's display name, in one place. Not translated: it is a name, and
 * the same everywhere it appears — the window title, the sign-in page, the
 * header of a shared link.
 */
export const PRODUCT_NAME = "PP Hub";

/**
 * A document title, translated and suffixed with the product name.
 *
 * Route `head` functions run outside React — there is no hook to call — so
 * they hand descriptors to the i18n singleton here rather than rendering
 * text at module scope, where the catalog does not exist yet.
 */
export function pageTitle(...parts: MessageDescriptor[]): string {
  return titleWithProduct(...parts.map((p) => i18n._(p)));
}

/** `<part> - PP Hub` — for titles made of text that is already a string, like a file name. */
export function titleWithProduct(...parts: string[]): string {
  return [...parts, PRODUCT_NAME].join(" - ");
}

/** Load a catalog and make it current. Idempotent. */
export async function activateLocale(locale: Locale): Promise<void> {
  if (i18n.locale === locale) return;
  const { messages } = await catalogs[locale]();
  i18n.loadAndActivate({ locale, messages });
  document.documentElement.lang = locale;
}

export { i18n };
