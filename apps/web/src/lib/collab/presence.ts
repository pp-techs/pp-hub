/* eslint-disable lingui/no-unlocalized-strings -- hex colours */
/**
 * Presence colours. slash-editor's collaboration layer only accepts
 * `#RRGGBB` (anything else renders the caret transparent), and the theme's
 * own tokens are greyscale apart from `--primary`, so the palette is hex: the
 * 600 shades of Tailwind's hues, each dark enough for the white initials and
 * caret label on top and light enough to stand out on the dark background.
 * Blue is left out — it is `--primary`, the colour of links and selection.
 */
const PRESENCE_COLORS = [
  "#DB2777",
  "#059669",
  "#D97706",
  "#7C3AED",
  "#DC2626",
  "#0891B2",
  "#65A30D",
  "#C026D3",
  "#EA580C",
  "#4F46E5",
  "#0D9488",
  "#E11D48",
] as const;

/**
 * A user's presence colour: derived from their id (FNV-1a), so it is the
 * same in every tab and on every collaborator's screen without being stored
 * anywhere.
 */
export function presenceColor(userId: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < userId.length; i++) {
    hash ^= userId.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return PRESENCE_COLORS[hash % PRESENCE_COLORS.length]!;
}

/**
 * Up to two initials for an avatar: the first and last word of a display
 * name ("Bùi Đức Nhật" → "BN"), or of an email's local part split on its
 * punctuation ("nhat.bui@…" → "NB") when that is all there is.
 */
export function presenceInitials(name: string): string {
  const at = name.indexOf("@");
  const source = at > 0 ? name.slice(0, at) : name;
  const words = source.split(at > 0 ? /[\s._+-]+/ : /\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  const first = Array.from(words[0]!)[0]!;
  const last = words.length > 1 ? Array.from(words[words.length - 1]!)[0]! : "";
  return (first + last).toLocaleUpperCase();
}
