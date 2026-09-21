import { useCallback } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";

/**
 * Every settings pane, as it appears in the URL fragment.
 *
 * Flat and globally unique on purpose: reading the address is one string
 * compare and one table lookup, and a pane can be regrouped in the dialog
 * without invalidating a link somebody already pasted into a chat.
 */
export const SETTINGS_SLUGS = [
  "general",
  "profile",
  "security",
  "space",
  "members",
  "groups",
  "danger",
] as const;

export type SettingsSlug = (typeof SETTINGS_SLUGS)[number];

/** Shown when the address names no pane, or one that cannot be opened. */
export const DEFAULT_SETTINGS_SLUG: SettingsSlug = "general";

// eslint-disable-next-line lingui/no-unlocalized-strings -- URL fragment, never shown
const PREFIX = "settings";

function isSlug(value: string): value is SettingsSlug {
  return (SETTINGS_SLUGS as readonly string[]).includes(value);
}

/** The fragment for `navigate({ hash })` — TanStack works without the `#`. */
export function settingsHash(slug: SettingsSlug): string {
  return `${PREFIX}/${slug}`;
}

/**
 * `null` when the address is not a settings one, so the dialog stays shut.
 *
 * `{ slug: null }` when it is a settings address naming a pane this build does
 * not have. That is deliberately distinct from "closed": the bridge answers it
 * by replacing the address with the default pane, rather than opening an empty
 * dialog or silently ignoring a link.
 */
export function parseSettingsHash(hash: string): { slug: SettingsSlug | null } | null {
  const raw = hash.replace(/^#/, "");
  if (raw !== PREFIX && !raw.startsWith(`${PREFIX}/`)) return null;
  const slug = raw.slice(PREFIX.length + 1);
  return { slug: isSlug(slug) ? slug : null };
}

/** The settings address currently in the URL, or `null` while there is none. */
export function useSettingsHash() {
  const { hash } = useLocation();
  return parseSettingsHash(hash);
}

/**
 * The only three ways the settings dialog changes what it shows.
 *
 * Opening pushes, so Back closes the dialog. Switching pane replaces, so Back
 * does not have to walk out through every pane that was glanced at. Closing
 * replaces too, so dismissing the dialog does not leave a history entry whose
 * only content is "the dialog used to be open here".
 *
 * Closing is a *navigation*, never `SettingsModal.hide()`. The hash is the
 * single source of truth for which pane is open; hiding behind its back leaves
 * an address that reopens the dialog on the next reload.
 */
export function useSettingsNavigation() {
  const navigate = useNavigate();

  const openSettings = useCallback(
    (slug: SettingsSlug) => void navigate({ hash: settingsHash(slug) }),
    [navigate],
  );
  const selectSettingsPane = useCallback(
    (slug: SettingsSlug) => void navigate({ hash: settingsHash(slug), replace: true }),
    [navigate],
  );
  const closeSettings = useCallback(() => void navigate({ hash: "", replace: true }), [navigate]);

  return { openSettings, selectSettingsPane, closeSettings };
}
