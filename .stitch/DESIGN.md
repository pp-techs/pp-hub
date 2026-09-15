---
name: pp-hub
colors:
  surface: "#ffffff"
  surface-dim: "#f5f5f5"
  surface-bright: "#ffffff"
  surface-container-lowest: "#ffffff"
  surface-container-low: "#fafafa"
  surface-container: "#f5f5f5"
  surface-container-high: "#e5e5e5"
  surface-container-highest: "#d4d4d4"
  on-surface: "#0a0a0a"
  on-surface-variant: "#737373"
  inverse-surface: "#0a0a0a"
  inverse-on-surface: "#ffffff"
  outline: "#a1a1a1"
  outline-variant: "#e5e5e5"
  surface-tint: "#2980b9"
  primary: "#2980b9"
  on-primary: "#fafafa"
  primary-container: "#eaf2f8"
  on-primary-container: "#1f6a94"
  inverse-primary: "#63b1de"
  secondary: "#f5f5f5"
  on-secondary: "#171717"
  secondary-container: "#f5f5f5"
  on-secondary-container: "#171717"
  tertiary: "#737373"
  on-tertiary: "#ffffff"
  tertiary-container: "#e5e5e5"
  on-tertiary-container: "#404040"
  error: "#e7000b"
  on-error: "#ffffff"
  error-container: "#fdeaea"
  on-error-container: "#b3000a"
  background: "#ffffff"
  on-background: "#0a0a0a"
  surface-variant: "#f5f5f5"
typography:
  display-lg:
    fontFamily: Noto Sans
    fontSize: 20px
    fontWeight: "600"
    lineHeight: 28px
    letterSpacing: -0.01em
  headline-md:
    fontFamily: Noto Sans
    fontSize: 14px
    fontWeight: "500"
    lineHeight: 20px
    letterSpacing: -0.01em
  body-base:
    fontFamily: Noto Sans
    fontSize: 12px
    fontWeight: "400"
    lineHeight: 20px
    letterSpacing: "0"
  body-bold:
    fontFamily: Noto Sans
    fontSize: 12px
    fontWeight: "500"
    lineHeight: 20px
    letterSpacing: "0"
  label-caps:
    fontFamily: Noto Sans
    fontSize: 11px
    fontWeight: "500"
    lineHeight: 16px
    letterSpacing: 0.04em
  micro:
    fontFamily: Noto Sans
    fontSize: 10px
    fontWeight: "500"
    lineHeight: 14px
    letterSpacing: "0"
  field-text:
    fontFamily: Noto Sans
    fontSize: 14px
    fontWeight: "400"
    lineHeight: 20px
    letterSpacing: "0"
rounded:
  sm: 0.375rem
  DEFAULT: 0.5rem
  md: 0.5rem
  lg: 0.625rem
  xl: 0.875rem
  2xl: 1.125rem
  full: 9999px
spacing:
  unit: 4px
  xs: 4px
  sm: 8px
  md: 12px
  lg: 16px
  xl: 24px
  gutter: 8px
  margin-mobile: 16px
  margin-desktop: 16px
---

# Design System: pp-hub

Extracted from `packages/ui/src` (token and primitive source) and `apps/web/src`
(consumption patterns). There is no `./src` at the repository root — this is a bun
workspace monorepo, and the design system lives entirely in `packages/ui`.

**Stack:** React 19 · Vite+ · Tailwind CSS v4 (CSS-first, no `tailwind.config.js`) ·
shadcn/ui registry, style `base-mira`, baseColor `neutral` · Base UI (`@base-ui/react`)
primitives · `cva` variants · `lucide-react` icons · Noto Sans Variable, self-hosted via
`@fontsource-variable/noto-sans`.

---

## 1. Visual Theme & Atmosphere

pp-hub looks like a tool, not a destination. It is a self-hosted Google Drive
replacement, and its interface is built for people who open it forty times a day to find
a file — so the design spends nothing on welcome and everything on legibility at speed.
The canvas is pure white, the ink is near-black, and between them sits a strictly
achromatic neutral ramp with zero chroma: no warm cream, no cool slate, no temperature at
all. Into that silence drops exactly one color — a flat, slightly dusty cyan-blue
(`#2980b9`) that appears on the primary button, the selected row, and almost nowhere
else. When you see blue, something is actionable or something is selected. That is the
entire semantic burden the palette carries.

The defining quality is **density**. A default button is 28px tall with 12px text; a
badge is 20px tall with 10px text; menu items are 28px; table rows use 12px type. This is
deliberate compression — a file listing should show twenty rows, not six, and the eye
should travel down a column of names without the layout asking for attention. Depth is
correspondingly cheap: floating surfaces are drawn with a hairline `ring-1
ring-foreground/10` rather than a shadow ramp, and only true popovers (menus, selects)
add a modest `shadow-md`. Motion is nearly absent — a 100ms fade-and-zoom-95 on anything
that opens, and a 1px downward press on buttons. Nothing bounces, nothing slides in on
scroll. The result reads as quiet, engineered, and slightly severe: a filing cabinet
rendered in software, where the only warmth in the room is the blue on the Upload button.

---

## 2. Color Palette & Roles

All colors are CSS custom properties in `packages/ui/src/styles/globals.css`, authored in
`oklch` and exposed to Tailwind through an `@theme inline` block. The neutral ramp is
Tailwind's `neutral` scale (chroma `0`) expressed in oklch. Hex values below are exact
sRGB conversions.

### Primary Foundation

| Name               | Light     | Dark                    | Role                                                                                                    |
| :----------------- | :-------- | :---------------------- | :------------------------------------------------------------------------------------------------------ |
| **Paper White**    | `#ffffff` | `#0a0a0a`               | `--background` — the page ground                                                                        |
| **Lifted Surface** | `#ffffff` | `#171717`               | `--card`, `--popover` — panels and menus. Flush with the page in light; _lighter_ than the page in dark |
| **Rail Gray**      | `#fafafa` | `#171717`               | `--sidebar` — the drive shell's left rail, a half-step off the page                                     |
| **Quiet Fill**     | `#f5f5f5` | `#262626`               | `--muted`, `--secondary`, `--accent` — hover fills, inert blocks, menu highlight                        |
| **Hairline**       | `#e5e5e5` | `rgba(255,255,255,.10)` | `--border`, `--input` — every rule and field edge                                                       |

Note the inversion logic: in light, surfaces _recede_ toward the page; in dark, they
_lift_ above it. `--border` also changes kind, from a solid gray to a 10% white overlay,
so rules stay hairline-thin against the darker ground.

### Accent & Interactive

| Name            | Hex                   | Role                                                                                                                          |
| :-------------- | :-------------------- | :---------------------------------------------------------------------------------------------------------------------------- |
| **Belize Blue** | `#2980b9`             | `--primary` — the _only_ chromatic color. Primary buttons, selection, active nav, avatar status dot. Identical in both themes |
| **Deep Belize** | `#1f6a94`             | Derived. The accessible text-weight variant — use when the blue must be _type_ on white (see Functional States note)          |
| **Belize Wash** | `#eaf2f8`             | Derived. A tint for accent-on-light chips and rails                                                                           |
| **Focus Gray**  | `#a1a1a1` / `#737373` | `--ring` — the focus ring is neutral, not blue                                                                                |

**`--accent` is not the accent color.** It is shadcn's neutral hover-fill token
(`#f5f5f5`). Reach for `--primary` when you actually want blue. This trips people up.

### Typography & Text Hierarchy

| Name             | Light     | Dark      | Role                                                                                     |
| :--------------- | :-------- | :-------- | :--------------------------------------------------------------------------------------- |
| **Ink**          | `#0a0a0a` | `#fafafa` | `--foreground` — body text, file names, headings                                         |
| **Second Voice** | `#737373` | `#a1a1a1` | `--muted-foreground` — metadata, timestamps, sizes, owners, section labels, placeholders |
| **Reversed Ink** | `#ffffff` | `#0a0a0a` | Text on inverted surfaces — tooltips paint `bg-foreground` with `text-background`        |

Two levels of text, not three. Everything that isn't primary content is
`--muted-foreground`, and the system never introduces a third mid-gray.

### Functional States

| Name          | Light     | Dark      | Role                                    |
| :------------ | :-------- | :-------- | :-------------------------------------- |
| **Alert Red** | `#e7000b` | `#ff6467` | `--destructive` — trash, revoke, delete |

Destructive is rendered as a **tint, not a fill**: `bg-destructive/10
text-destructive`, deepening to `/20` on hover. Delete is reachable without shouting.

There is **no success or warning token**. If the product needs one (a completed upload, a
quota nearing its limit), it must be added to `globals.css` rather than inlined.

**Known contrast defects, carried from the source:**

- White (`#fafafa`) on `#2980b9` measures **4.12:1**, and primary button text is 12px —
  below the 4.5:1 WCAG AA threshold for normal text. Darkening `--primary` to ≈`#22699a`
  reaches 5.9:1 while reading as the same blue.
- `--primary-foreground` inverts meaning between themes: near-white in light, near-black
  (`#171717`, **4.17:1**) in dark, on the same blue fill.
- `--sidebar-primary` is a saturated blue (`#1447e6`) in dark only, against a neutral
  `#171717` in light — an unswapped shadcn default, and the one place a second hue leaks
  in.
- `--chart-1..5` are all achromatic (`#d4d4d4` → `#262626`). Nothing charts yet; any real
  data visualization needs a categorical scale built first.

---

## 3. Typography Rules

One family does everything. **Noto Sans Variable**, self-hosted through
`@fontsource-variable/noto-sans` — the app never calls a font CDN, which is consistent
with a product whose entire pitch is self-hosting. Noto Sans is a humanist sans with a
large x-height, open apertures, and unremarkable letterforms; it is chosen for legibility
at 10–12px and for having no opinion of its own, which is exactly right for an interface
that displays user-supplied file names.

`--font-heading` is declared and currently resolves to `--font-sans`. It is a seam, not a
second face: a display family can be introduced in one line later. `CardTitle` and
`EmptyTitle` already route through it.

### Hierarchy & Weights

| Role                         | Size / Weight                       | Where it appears                                                         |
| :--------------------------- | :---------------------------------- | :----------------------------------------------------------------------- |
| **Page heading**             | 20px · 600 · tight tracking         | Landing and empty-state headlines ("Welcome to pp-hub")                  |
| **Panel title**              | 14px · 500                          | `CardTitle`, `DialogTitle`, `EmptyTitle` — via `font-heading`            |
| **Body / component default** | 12px · 400 · line-height 1.625      | Buttons, cards, dialogs, table rows, menu items — the workhorse          |
| **Emphasis**                 | 12px · 500                          | File names in a row, active nav, table headers                           |
| **Section label**            | 10–11px · 500 · uppercase · +0.04em | Sidebar groups ("My Drive", "Folders", "Files"), space role chips        |
| **Micro**                    | 10px · 500                          | `Badge`, `size="xs"` buttons and toggles                                 |
| **Field text**               | 14px, dropping to 12px at `md:`     | Inputs and textareas — the larger mobile size prevents iOS zoom-on-focus |

Weights run 400 / 500 / 600 only. There is no 700 anywhere in the system; emphasis is
carried by color and size, not by boldness.

### Spacing Principles

- Line-height is `relaxed` (1.625) on 12px body text — generous leading at a small size is
  what keeps the density readable rather than cramped.
- Uppercase labels always pair with `--muted-foreground` and a modest positive tracking.
  Uppercase is never used at body size.
- Titles use slight negative tracking (`tracking-tight`); body text uses none.
- Columns of numbers (file sizes, dates) want `tabular-nums`.

---

## 4. Component Stylings

Primitives are **Base UI**, not Radix. Two conventions differ from Radix muscle memory:
polymorphism is a `render` prop rather than `asChild`, and icon side is declared with
`data-icon="inline-start" | "inline-end"` — the `cva` reads that attribute to tighten the
padding on that side automatically.

### Buttons

Radius `rounded-md` (8px). Six variants:

| Variant       | Treatment                                                                 |
| :------------ | :------------------------------------------------------------------------ |
| `default`     | Solid `#2980b9` fill, near-white text, hover at 80% opacity               |
| `outline`     | Hairline `--border`, transparent, hover fills `input/50`                  |
| `secondary`   | `#f5f5f5` fill, hover mixes 5% foreground in via `color-mix(in oklch, …)` |
| `ghost`       | No chrome until hover, then `--muted`                                     |
| `destructive` | Red at 10% opacity with red text — a tint, never a solid red button       |
| `link`        | Blue text, underline on hover, 4px underline offset                       |

Eight sizes, all short: `default` 28px, `xs` 20px, `sm` 24px, `lg` 32px, plus four square
icon sizes (20 / 24 / 28 / 32px). Icon glyphs are auto-scaled by the size variant — 3.5
units at default, 2.5 at `xs` — so never set a size on a lucide icon.

Every variant **presses down 1px on `:active`** (`translate-y-px`), suppressed on anything
that opens a popup. Focus is a 2px ring at 30% opacity plus a `--ring`-colored border.
Invalid state swaps both to `--destructive`.

### Cards & Surfaces

Cards are `rounded-lg` (10px) on `--card`, edged with `ring-1 ring-foreground/10` rather
than a border — a ring stays a true hairline at any radius and needs no dark-mode variant.
Internal rhythm is driven by a single CSS variable, `--card-spacing` (16px, or 12px at
`size="sm"`), which sets the padding _and_ the gap between header / content / footer, so
slots stay aligned without anyone hand-setting padding. A first-child image goes
full-bleed to the top corners automatically.

Dialogs are `rounded-xl` (14px) on `--popover`, `p-4`, `sm:max-w-sm`, with the same ring.
Alert dialogs are narrower (`max-w-xs`, `max-w-64` at `size="sm"`), center their content
on mobile and left-align it from `sm:` up, and reverse their footer to a vertical stack
below `sm:` so the confirming action sits on top under the thumb. The overlay is
`bg-black/80` with a feature-queried `backdrop-blur-xs`.

Menus, selects and context menus are `rounded-lg`, `p-1`, and are the **only** surfaces
that carry a real shadow (`shadow-md`) on top of the ring. Their items are `min-h-7`,
`rounded-md`, `px-2 py-1`, highlighting with `--accent` on focus.

### Navigation

The drive shell is a fixed **256px** (`w-64`) left rail on `--sidebar` with a `border-r`,
divided into three horizontal bands by rules: a bordered team switcher at the top, a
scrolling nav in the middle, and the user menu pinned to the bottom by a `border-t`. The
main column is `flex min-w-0 flex-1` — the `min-w-0` is load-bearing, since without it a
long file name blows out the layout.

Nav items are `ghost` buttons, left-aligned, in `--muted-foreground` until active. Group
headers are the uppercase 10–11px label style. The active space is marked by a filled
`--sidebar-accent` background plus `font-medium`, with its role (`owner` / `editor` /
`viewer`) set right-aligned in 10px uppercase muted type. There is no mobile drawer — the
rail does not collapse.

### Inputs & Forms

Height 28px, `rounded-md`, hairline `border-input`, and — distinctively — a **translucent
fill** (`bg-input/20`, deepening to `/30` in dark) rather than white or a solid gray. The
field reads as a recessed well rather than a raised box. Focus raises a 2px
`ring-ring/30`; `aria-invalid` swaps the border and ring to `--destructive`.

Textareas add `field-sizing-content` with `min-h-16` and `resize-none` — they grow with
their content, and the user never drags a resize handle.

Labels are 12px / 500 with `leading-none`, and dim to 50% opacity when their control is
disabled.

Toggle groups collapse their seams: adjacent members drop the inner radius and the inner
border so a segmented control reads as one object. The drive's Grid/List switcher is
built this way, from two `icon-xs` ghost buttons inside a bordered `ButtonGroup`.

### Domain-Specific: the file browser

The densest surface in the product, and the one that sets the house style.

**List view** — a `text-xs` table, columns **Name · Modified · Size · Owner** plus a
trailing action cluster. Header cells are 40px, `font-medium`, `whitespace-nowrap`. Rows
carry a `border-b` hairline, fill `muted/50` on hover, and `--muted` when selected.
Actions are shown inline per row and only when they apply — `canEdit()` and
`canPreview()` decide, so a `.docx` shows Edit but no preview while an `.rtf` shows both.

**Grid view** — file cards on a responsive grid: `grid-cols-2` climbing to `sm:3`,
`lg:4`, `xl:5`, with a 12px gutter. Folders and files are split into two labelled
sections, each headed by a `size-4` lucide glyph plus an uppercase section label, with a
24px gap between the two bands.

**Empty state** — a dashed-border `rounded-xl` panel, centered, `gap-4`, holding a muted
`size-10` glyph, a 14px title, and a `max-w-sm` line of guidance. "This folder is empty /
Upload a file or create a folder to get started."

**Avatars** — `rounded-full`, 24 / 32 / 40px, with a clever edge: the border is an
`::after` ring that uses `mix-blend-darken` in light and `mix-blend-lighten` in dark, so
it reads against any photo underneath. A status dot sits bottom-right in `--primary`,
ringed 2px in the page background to punch it out of the image.

**Tooltips** invert: `bg-foreground` / `text-background`, `rounded-md`, `px-3 py-1.5`,
with a 10px rotated square as the arrow.

---

## 5. Layout Principles

### Grid & Structure

The application is an **app shell**, not a document: a full-viewport `min-h-svh` flex row
of a fixed 256px rail and a fluid main column. There is no centered content container and
no max-width on the working surface — file listings use all available width, and
`max-w-*` appears only on dialogs (`sm`), empty-state copy (`sm`), and the preview modal
(`90vw × 85vh`). Content areas scroll independently via `min-h-0 flex-1 overflow-auto`,
so the rail and toolbars stay fixed while the listing moves.

Breakpoints are Tailwind's stock set — `sm` 640, `md` 768, `lg` 1024, `xl` 1280 — with no
custom `@theme` override.

### Whitespace Strategy

A 4px base unit, with **8px as the workhorse**. Counting actual usage across the app:
`gap-2` (8px) is the most common value by a wide margin, followed by `gap-3` (12px) and
`gap-1` (4px); container padding lands on `p-2` (8px) for rails and toolbars, `p-4` (16px)
for content areas, and `p-6` (24px) only for full-page centered states.

Spacing is always `gap` on a flex or grid parent — the codebase almost never sets per-child
margins, and `Card` pushes this further by deriving its own padding and inter-slot gap from
a single `--card-spacing` variable.

The distance between a label and its field is 4px; between form groups, 12px; between major
bands of a listing, 24px. Nothing in the app uses section spacing above 48px.

### Alignment & Visual Balance

Left-aligned throughout. Centering is reserved for states where there is nothing to align
_to_ — the empty folder, the 404, the sign-in card, the no-spaces landing. Those center both
axes, stack at `gap-4`, and end with exactly one primary action.

Toolbars are a left cluster of actions and a right cluster of view controls, separated by a
4px-tall vertical `Separator`. Metadata columns are right-shouldered against a fixed-width
action cluster so the actions form a clean vertical edge down the listing.

### Responsive Behavior & Touch

Honestly: this is a **desktop-first** interface, and the responsive work is partial. The
file grid reflows properly (2 → 3 → 4 → 5 columns), dialogs cap at `sm:max-w-sm`, alert
footers restack, and inputs use 14px text below `md:` specifically to defeat iOS
zoom-on-focus. But the 256px rail never collapses, there is no drawer or bottom nav, and
the densest controls — 20px `xs` buttons, 20px badges — are well under the 44px touch
minimum. Treat mobile as a viewing context, not a working one, until a rail-collapse
pattern exists.

---

## 6. Design System Notes for Stitch Generation

### Language to Use

Describe pp-hub screens as: _dense, achromatic, engineered. Pure white ground, near-black
text, one flat cyan-blue accent and nothing else chromatic. Hairline rules instead of
shadows. Small type — 12px body, 10–11px uppercase labels. Compact controls, 28px tall.
Left-aligned, tool-like, no decoration._

Avoid asking for: gradients, glassmorphism, large heroes, generous whitespace, colored
section backgrounds, illustration, multiple accent hues, or rounded pill-shaped cards.
Every one of those fights the system.

### Color References

- **Belize Blue `#2980b9`** — the single accent. Primary buttons, selection, active state.
- **Paper White `#ffffff`** / **Ink `#0a0a0a`** — ground and text.
- **Quiet Fill `#f5f5f5`** — hover states and inert blocks.
- **Hairline `#e5e5e5`** — every rule, border and field edge.
- **Second Voice `#737373`** — all metadata and labels.
- **Alert Red `#e7000b`**, always at 10% opacity as a tint.

Dark theme swaps ground to `#0a0a0a` and _lifts_ panels to `#171717`; borders become white
at 10%; the blue does not change.

### Component Prompts

> A file browser list view on white. Fixed 256px left sidebar in near-white with a hairline
> right border, holding a space switcher, an uppercase 11px "My Drive" group label, and a
> folder tree. Main area: a breadcrumb row, a toolbar with a small blue Upload button and a
> two-icon grid/list segmented control on the right, then a dense table with columns Name,
> Modified, Size, Owner at 12px, hairline row rules, and small ghost action buttons at the
> end of each row.

> A share dialog, 384px wide, 14px rounded corners, white, hairline ring, 16px padding.
> Sections titled "Invite people" and "Public link" at 14px medium. An email field with a
> recessed translucent fill, a three-way segmented control reading Viewer / Download /
> Editor, and a "Current access" list below. Primary button in flat cyan-blue, 28px tall.

> An empty folder state. Centered in the content area: a dashed-border rounded panel, a
> muted 40px folder outline icon, "This folder is empty" at 14px medium, one line of 12px
> muted guidance, and a single blue Upload button.

### Incremental Iteration

- Fix density first. If a generated screen feels roomy, halve the control heights and drop
  body type to 12px before touching anything else — density is the system's signature.
- Then strip color. Generated designs reliably add a second hue; remove it and re-express
  that distinction with weight or a neutral fill.
- Then replace shadows with `ring`-style hairlines, except on dropdowns and selects, which
  legitimately carry a soft shadow.
- Keep copy in sentence case, name things the way a person would ("Shared with me", "Move
  to trash"), and let real terminology stand: spaces, nodes, versions, shares, previews.
