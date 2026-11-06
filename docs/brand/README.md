# PP Hub brand

**Megaminx Hub** — a twelve-faced megaminx: every face a space, every piece a file, the solid
centre piece is the hub. The wordmark `pp hub` is constructed lowercase geometry (no font
licence involved).

## Files

| File (`logo/`)                               | Use                                                                  |
| -------------------------------------------- | -------------------------------------------------------------------- |
| `pp-hub-symbol.svg`                          | Primary symbol, ≥ 48 px                                              |
| `pp-hub-symbol-small.svg`                    | Small cut (front face only), 24–48 px; source of app/touch/PWA icons |
| `pp-hub-symbol-tiny.svg`                     | Silhouette cut (no inner pieces), 16 px favicon frame only           |
| `pp-hub-horizontal.svg`                      | Default lockup (headers, docs, website)                              |
| `pp-hub-stacked.svg`                         | Square-ish spaces (splash, social)                                   |
| `pp-hub-wordmark.svg`                        | Wordmark alone, when the symbol is already present nearby            |
| `*-reversed.svg`, `pp-hub-symbol-onblue.svg` | White, for dark or brand-blue backgrounds                            |

`export/` holds black / white / mono variants with PNGs, and `export/symbol/` the web icon set
installed in `apps/web/public/`. `board/` is the presentation (`board.html`, `slides/`).

## Colour

| Role              | HEX       | RGB        | Notes                                                         |
| ----------------- | --------- | ---------- | ------------------------------------------------------------- |
| Hub Blue (symbol) | `#2980B9` | 41 128 185 | Same as the UI `--primary`; ≈ Pantone 7461 C, CMYK 78 31 0 27 |
| Ink (wordmark)    | `#0F1B2A` | 15 27 42   | Text on light backgrounds                                     |
| White             | `#FFFFFF` |            | Reversed marks                                                |

One-colour (black, white, Hub Blue) versions are approved. Do not use gradients, shadows or
other colours on the mark.

## Clear space and minimum size

- Clear space on every side: the width of the solid centre pentagon (≈ ¼ of symbol width).
- Symbol: primary ≥ 48 px, small cut 24–48 px, tiny cut 16 px.
- Horizontal lockup: ≥ 96 px wide. Print: symbol ≥ 8 mm.

## Misuse

Don't rotate (the pentagon points up), stretch, re-draw the pieces, outline the mark, put it on
busy photos without a solid panel, set "pp hub" in another typeface, or recolour individual
pieces.

## In the app

- `BrandMark` (`apps/web/src/components/brand-mark.tsx`) renders `public/brand-mark.svg`.
- `index.html` links `favicon.ico` (16 tiny / 32 / 48 small cut), PNG favicons, the touch icon and
  `site.webmanifest` (192, 512, maskable 512); `theme-color` is Hub Blue.

## Open items

- Trademark clearance has not been done — run a professional search before external use.
- Megaminx/dodecahedron forms exist elsewhere; the solid hub centre is the ownable detail.
