/* eslint-disable lingui/no-unlocalized-strings -- digit alphabet, not copy */
// Fractional indexing, ported byte for byte from the kanban server's
// `position.go`: boards made before Kanban became a packaged app keep their
// position strings, so new keys must sort among them exactly as the server's
// did. A position is a string over a 36-letter alphabet read as the digits of a
// fraction, so between any two positions there is another one, and ordering
// them bytewise (JS `<` on these ASCII strings) orders the items. Keys never
// end in the zero digit, which is what lets a midpoint always exist.

const ALPHABET = "0123456789abcdefghijklmnopqrstuvwxyz";
const BASE = ALPHABET.length;
const CODE_0 = 48; // "0"
const CODE_A = 97; // "a"
const CODE_Z = 122; // "z"
const CODE_9 = 57; // "9"

function digit(s: string, i: number): number {
  if (i >= s.length) return 0;
  // The alphabet is in byte order, so a letter's index is its offset.
  const c = s.charCodeAt(i);
  return c >= CODE_A ? c - CODE_A + 10 : c - CODE_0;
}

/** A position that sorts after `a` and before `b`; empty `a` is the start, empty `b` the end. `a` must sort before `b`. */
export function between(a: string, b: string): string {
  if (b !== "") {
    // Strip the shared prefix: `a` is read as padded with zeros.
    let n = 0;
    while (n < b.length && digit(a, n) === digit(b, n)) n++;
    if (n > 0) return b.slice(0, n) + between(n < a.length ? a.slice(n) : "", b.slice(n));
  }
  const da = digit(a, 0);
  const db = b === "" ? BASE : digit(b, 0);
  if (db - da > 1) return ALPHABET.charAt(Math.floor((da + db + 1) / 2));
  // Consecutive first digits.
  if (b.length > 1) return b.slice(0, 1);
  return ALPHABET.charAt(da) + between(a.length > 1 ? a.slice(1) : "", "");
}

// Counter keys are what appending uses: a letter for the number of digits, the
// number in base 36 that wide, and a closing "i" (a last letter other than the
// zero digit keeps a key usable by `between`). They sort in counting order, and
// a key gains a letter only when the digits are used up.
const COUNTER_TAIL = "i";

function counterKey(v: number): string {
  let width = 1;
  for (let pow = BASE; v >= pow; pow *= BASE) width++;
  const digits: string[] = [];
  for (let j = 0; j < width; j++) {
    digits.unshift(ALPHABET.charAt(v % BASE));
    v = Math.floor(v / BASE);
  }
  return ALPHABET.charAt(width) + digits.join("") + COUNTER_TAIL;
}

/** Reads a counter key; null for any other key. */
function counterValue(s: string): number | null {
  if (s.length < 3 || s.charAt(s.length - 1) !== COUNTER_TAIL) return null;
  const width = digit(s, 0);
  if (width < 1 || width > 6 || s.length !== width + 2) return null;
  let v = 0;
  for (let j = 1; j <= width; j++) {
    const c = s.charCodeAt(j);
    if (!((c >= CODE_0 && c <= CODE_9) || (c >= CODE_A && c <= CODE_Z))) return null;
    v = v * BASE + digit(s, j);
  }
  return v;
}

/** A position after the last one of a list; "" is an empty list. */
export function after(last: string): string {
  if (last === "") return counterKey(1);
  const v = counterValue(last);
  if (v !== null) return counterKey(v + 1);
  // The end of the list was placed by hand, between two others.
  return between(last, "");
}

/**
 * The length past which a position is renumbered rather than halved further;
 * repeated inserts at one spot lengthen a key by a letter every few moves.
 */
export const MAX_POSITION_LEN = 48;

/** `n` ordered short positions, for renumbering a list whose keys have grown long. */
export function spread(n: number): string[] {
  return Array.from({ length: n }, (_, i) => counterKey(i + 1));
}
