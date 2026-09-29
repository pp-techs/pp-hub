/* eslint-disable lingui/no-unlocalized-strings -- hex colours */
/**
 * Caret colours. slash-editor's collaboration layer only accepts `#RRGGBB`
 * (anything else renders transparent), so these are hex rather than theme
 * tokens. All are dark enough for the white caret label and light enough to
 * read on the dark theme.
 */
const CARET_COLORS = [
  "#2563EB",
  "#DB2777",
  "#059669",
  "#D97706",
  "#7C3AED",
  "#DC2626",
  "#0891B2",
  "#65A30D",
  "#C026D3",
  "#EA580C",
] as const;

/**
 * A user's caret colour: derived from their id, so it is the same in every
 * tab and on every collaborator's screen without being stored anywhere.
 */
export function presenceColor(userId: string): string {
  let hash = 0;
  for (let i = 0; i < userId.length; i++) hash = (hash * 31 + userId.charCodeAt(i)) >>> 0;
  return CARET_COLORS[hash % CARET_COLORS.length]!;
}
