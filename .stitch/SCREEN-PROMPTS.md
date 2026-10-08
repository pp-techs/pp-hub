# pp-hub — Stitch screen plan

Design system and screens for the pp-hub UI redesign.

## Stitch resources

| Thing                           | ID                                                    |
| :------------------------------ | :---------------------------------------------------- |
| Project 1 (primary)             | `11969748414038004354` — "pp-hub — Self-hosted Drive" |
| Project 2 (spillover, empty)    | `1906209590246049865`                                 |
| DESIGN.md screen (project 1)    | `11426701925242382693`                                |
| Design system asset (project 1) | `assets/85a1b28154ad4068bed8af7a41ee83b2`             |
| Design system asset (project 2) | `assets/f67cbf47e9024f648360318991bbf618`             |

## Status

Done and on the canvas of project 1:

1. Drive — list view
2. Drive — grid view with details panel
3. Command palette & search
4. Share dialog

Blocked: generation stopped attaching screens to the project after the 4th, and
later calls stopped returning at all — an account-level Stitch quota, not a
per-project cap (a brand-new project refused screens too). The ten prompts below
are ready to re-run once the quota resets.

## How to re-run

Generate **one screen per call, serially**. Parallel calls race on the project
canvas: in the first run four concurrent generations each returned `COMPLETE`
with a rendered screenshot, but only one of them ended up in `screenInstances`.
After each call, confirm with `list_screens` before starting the next.

```
mcp__stitch__generate_screen_from_text(
  projectId   = "11969748414038004354",
  deviceType  = "DESKTOP",
  designSystem= "assets/85a1b28154ad4068bed8af7a41ee83b2",
  prompt      = <one block below>
)
```

Every prompt opens with the same house style line, which is the part that keeps
generations from drifting: dense, achromatic, engineered; pure white ground,
near-black text, one flat cyan-blue accent `#2980b9` and nothing else chromatic;
hairline `#e5e5e5` rules instead of shadows; 12px body, 10–11px uppercase muted
labels; 28px controls; left-aligned, no decoration.

---

## 5. Upload — drop overlay & transfer tray

Screen name: "Upload — drop overlay & transfer tray". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration.

The drive list view mid-upload, with a drag-and-drop in progress.

Base layer: the standard shell — fixed 256px left rail on #fafafa with hairline right border (space switcher "Acme Core", search field with ⌘K hint, uppercase QUICK ACCESS group with Recent / Starred / Shared with me / Trash, uppercase MY DRIVE folder tree with "Invoices" active, uppercase SPACES group, pinned bottom storage meter and user row); main column with a 40px breadcrumb header "Acme Core / Documents / Invoices", a 40px toolbar with a cyan-blue Upload button, outline New folder and New document buttons, filter chips, and a grid/list toggle on the right, and a dense 12px file table beneath.

DROP OVERLAY: the entire main column content area is covered by a drop target — a cyan-tinted wash (#eaf2f8) at high transparency with a 2px dashed cyan-blue border inset 8px and 10px rounded corners. Centred within it: a 40px muted-cyan outline upload-cloud glyph, then "Drop files to upload" at 14px medium near-black, then a 12px muted line "They will be added to Acme Core / Documents / Invoices", then a 10px muted line "Folders are uploaded with their structure preserved". The file table remains faintly visible through the wash.

TRANSFER TRAY, anchored bottom-right of the viewport above everything, 360px wide, white, 10px rounded corners, hairline ring plus a modest soft shadow:

- A 36px header with a hairline bottom border: a 14px medium title "Uploading 3 of 7", a muted 10px line beneath reading "About 40 seconds left · 12.4 MB/s", and on the right two ghost icon buttons, a chevron-down to collapse and an ×.
- A 2px-tall progress track spanning the full width directly under the header, light gray with a cyan-blue fill about 45% across.
- A list of seven 40px rows, each with a hairline bottom rule: a 20px muted file-type glyph, then a 12px truncated name with a 10px muted status line beneath, then a right-aligned state element.
  Row 1 "Q1-forecast-final.xlsx" / "1.2 MB" — complete: a small cyan-blue check glyph on the right.
  Row 2 "Board-deck-v9.pptx" / "8.7 MB" — complete: check glyph.
  Row 3 "walkthrough.mp4" / "68.2 MB of 184 MB · 12.4 MB/s" — in progress: a thin 2px cyan progress bar spanning the row's lower edge at 37%, and a right-aligned muted "37%" in tabular figures plus a ghost pause button.
  Row 4 "Scan_0042.png" / "2.4 MB" — in progress: a 16px circular cyan determinate spinner and a muted "82%".
  Row 5 "archive-2025.zip" / "Waiting" — queued: a muted 10px "Queued" chip and a ghost × button.
  Row 6 "contract-draft.docx" / "Waiting" — queued: same treatment.
  Row 7 "budget-2024.csv" / "Failed · file exceeds the 2 GB limit" — the status line is in alert red, the row has a faint red-tinted background at 10%, and a small red-tinted ghost "Retry" button sits on the right beside an ×.
- A 32px footer with a hairline top border: a ghost text button "Pause all" on the left and a ghost text button "Clear completed" on the right, both 12px muted.

---

## 6. Preview modal & version history

Screen name: "Preview modal & version history". Dense, achromatic, engineered. Near-black chrome, one flat cyan-blue accent (#2980b9), hairline rules, 12px body type, 10-11px uppercase muted labels, compact 28px controls. No gradients, no illustration.

A full-viewport file preview lightbox over a #0a0a0a backdrop at 92% opacity.

TOP BAR, 48px, transparent over the dark backdrop, white text:

- Left: a ghost × close button, then a 20px outline PDF glyph, then "brand-guide.pdf" at 14px medium in white with a 10px muted-gray line beneath reading "Acme Core / Design · 8.1 MB · version 4".
- Centre: a compact pager — a ghost left-chevron button, the text "7 of 24" at 12px in tabular figures, and a ghost right-chevron button.
- Right: a cluster of ghost icon buttons in white — star, comment with a small cyan "3" badge, share, download, print, open-in-new — then a vertical hairline, then an ellipsis.

STAGE, centre, fills the remaining height: a rendered page of a PDF brand manual displayed as a white page with a hairline edge, letterboxed on the dark ground, showing real-looking content — a heading, two columns of body copy, a row of color swatches, and a small bar chart. It is scaled to fit with generous dark margin on either side. A left chevron and a right chevron sit as 32px circular semi-transparent dark buttons vertically centred against the left and right edges of the stage.

FLOATING ZOOM TOOLBAR, anchored bottom-centre over the stage: a dark 36px-tall rounded rectangle with a hairline white-at-10% ring, holding ghost white icon buttons — minus, the text "125%" at 12px tabular, plus, a vertical hairline, fit-to-width, actual-size, rotate, and a fullscreen glyph.

RIGHT PANEL, fixed 320px, white, hairline left border, full height, so the dark stage ends where it begins:

- A 36px tab strip with a hairline bottom border holding three 12px tabs — "Details", "Comments", "Versions" — with "Versions" active, marked by a 2px cyan-blue underline and near-black medium text while the others are muted.
- Under it, the Versions content: an uppercase 11px muted label "VERSION HISTORY" with a small ghost "Upload new version" button on the right. Then a vertical list of four version entries connected by a hairline vertical rule with small dots. Each entry is a 12px block: a row with "Version 4" in medium plus a small cyan "Current" badge, then a 10px muted line "Mai Tran · Mar 12, 2026 14:08 · 8.1 MB", then a row of three tiny ghost text buttons "Preview", "Download", "Restore". Subsequent entries, without the badge: "Version 3 / Mai Tran · Mar 09, 2026 10:22 · 7.9 MB"; "Version 2 / Nguyen Bui · Mar 04, 2026 16:40 · 7.2 MB"; "Version 1 / Mai Tran · Feb 02, 2026 09:15 · 6.8 MB". Sizes use tabular figures. A muted 10px footer line reads "Versions are immutable. Restoring makes the selected version current."

---

## 7. Empty states

Screen name: "Empty states". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration, no decoration.

A single desktop screen presenting four empty states as a reference sheet, arranged in a 2×2 grid with a 24px gap and 24px page padding, each cell a white panel with a hairline ring and 10px rounded corners so they read as separate specimens. Above each cell sits a small uppercase 11px muted caption label naming it.

Cell 1, captioned "EMPTY FOLDER": the drive content area in miniature — a 40px breadcrumb strip "Acme Core / Documents / Q1 2026" and a 40px toolbar with a cyan-blue Upload button and a grid/list toggle along the top, then a centred empty state filling the rest: a dashed hairline border in a 14px-rounded rectangle, inset, centred content with a 16px gap — a 40px muted outline folder glyph, "This folder is empty" at 14px medium, a 12px muted line capped at 24rem reading "Upload a file or create a folder to get started. You can also drag files straight onto this area.", and a row of two buttons, a solid cyan-blue "Upload files" and an outline "New folder".

Cell 2, captioned "NO SEARCH RESULTS": a 40px strip holding a search field containing the query "quarterly report q3" with a clear ×, then a centred state — a 40px muted outline magnifier glyph, "No files match “quarterly report q3”" at 14px medium, a 12px muted line "Try fewer words, check the spelling, or search across all spaces.", and a row of an outline "Clear filters" button and a ghost "Search all spaces" button. Beneath, an uppercase 11px muted label "ACTIVE FILTERS" over three small removable filter chips each with an ×: "Type: Spreadsheet", "Owner: Mai Tran", "Modified: Last 7 days".

Cell 3, captioned "EMPTY TRASH": a centred state — a 40px muted outline trash glyph, "Trash is empty" at 14px medium, a 12px muted line "Items you delete land here for 30 days before they are removed permanently.", and a single outline "Go to My Drive" button. A 10px muted footnote below reads "Deleted items still count toward your storage until they are purged."

Cell 4, captioned "NO SPACES YET": a centred onboarding state — a 40px muted outline layers glyph, "Welcome to pp-hub" at 20px semibold with tight tracking, a 12px muted line capped at 24rem reading "Spaces are isolated file areas with their own members and permissions. Create one to start uploading.", a solid cyan-blue "Create a space" button, and beneath it a hairline top rule with a 12px muted line "Waiting on an invite? Ask a space owner to add you, then refresh." plus a cyan-blue link-style "Refresh" button.

---

## 8. Space settings — members

Screen name: "Space settings — members". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration.

The standard shell: fixed 256px left rail on #fafafa with hairline right border holding the space switcher "Acme Core / owner", a search field with ⌘K hint, uppercase QUICK ACCESS and MY DRIVE and SPACES groups, and a pinned bottom storage meter plus user row.

MAIN COLUMN:

- A 40px header with hairline bottom border: breadcrumbs "Acme Core / Settings" with the last crumb near-black, and right-side ghost icon buttons.
- A settings sub-navigation rendered as a horizontal tab strip, 36px tall, hairline bottom border, 16px left padding: six 12px tabs — "General", "Members", "Sharing", "Storage", "Integrations", "Danger zone" — with "Members" active, marked by a 2px cyan-blue underline and near-black medium text; the others muted. "Danger zone" is in muted red.
- Content area, 16px padding, max width none.
  - A header row: "Members" at 14px medium with a muted 10px count "12 members · 3 pending" beneath, and on the right a solid cyan-blue "Invite people" button 28px tall with a user-plus glyph, plus an outline "Export CSV" button.
  - A filter row: a 28px recessed search input with placeholder "Filter by name or email", then bordered selects "Role: All" and "Status: All", and on the far right a muted 10px "Showing 12 of 12".
  - A dense 12px table. A 40px header row, medium, muted, with a checkbox cell then columns Member, Role, Joined, Last active, Files owned, and a trailing action column. Twelve 40px rows with hairline bottom rules:
    Each Member cell holds a 24px circular avatar, a 12px medium name, and a 10px muted email beneath. Role cells are compact bordered selects with a caret. Joined and Last active are muted with tabular figures. Files owned is a right-aligned tabular number. The action cell holds a ghost ellipsis button.
    Rows: "Nguyen Bui / nguyen@acme.dev" · Owner rendered as plain muted text with no select · Jan 04, 2026 · 2 min ago · 1,284 — its row carries a small muted "You" chip after the name.
    "Mai Tran / mai@acme.dev" · Editor · Jan 04, 2026 · 18 min ago · 942.
    "Le Hoang / le@acme.dev" · Editor · Jan 11, 2026 · 3 hours ago · 617.
    "Pham Anh / anh@acme.dev" · Viewer · Feb 02, 2026 · Yesterday · 0.
    "Tran Quoc / quoc@acme.dev" · Editor · Feb 14, 2026 · 2 days ago · 88.
    "Do Linh / linh@acme.dev" · Viewer · Feb 20, 2026 · 6 days ago · 4.
    "Vu Minh / minh@acme.dev" · Viewer · Mar 01, 2026 · 11 days ago · 0.
    "Hoang Yen / yen@partner.io" · Viewer · Mar 03, 2026 · Mar 09 · 0 — with a small muted "External" chip after the name.
    "Bui Nam / nam@acme.dev" · Editor · Mar 05, 2026 · Mar 10 · 31.
    Then three pending invitations, visually distinct: their avatar is a light-gray circle holding an envelope glyph, the name cell shows only the email in muted italic, a small cyan-tinted "Pending" chip sits beside it, and the Joined column reads "Invited Mar 12" while Last active shows an em dash. Their action cell shows two tiny ghost text buttons, "Resend" and a red-tinted "Revoke". Rows: "thu@acme.dev" · Viewer; "khanh@acme.dev" · Editor; "contractor@vendor.co" · Viewer with an "External" chip.
  - Below the table, an uppercase 11px muted label "ROLE REFERENCE" over a hairline-bordered rounded-lg panel with three columns separated by vertical hairlines, each column a 12px medium role name over a short muted 12px list of permissions: "Owner — Manage members and settings, delete the space, everything an editor can do"; "Editor — Upload, edit, move, delete, share files, restore versions"; "Viewer — View and download files, nothing else".

---

## 9. Office editor — collaborative document

Screen name: "Office editor — collaborative document". Dense, achromatic, engineered chrome wrapping a document editing surface. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px chrome type, 28px controls. No gradients, no illustration.

A full-page in-browser office editor, no left rail — the editor takes the whole viewport.

TOP CHROME BAR, 48px, white, hairline bottom border:

- Left: a ghost back-arrow button, then a 20px muted outline document glyph, then a two-line block — "Vendor terms.docx" at 14px medium with a small ghost pencil glyph beside it for rename, and a 10px muted line beneath reading "Acme Core / Documents / Invoices · Saved to version 12 · 2 seconds ago" where "Saved" carries a tiny cyan check glyph.
- Right: a presence cluster of four overlapping 24px circular avatars each ringed 2px in white, followed by a muted 10px "+2"; then a vertical hairline; then an outline "Share" button 28px tall with a user-plus glyph, a ghost "Version history" button with a clock glyph, a ghost comment glyph with a small cyan "3" badge, a ghost download glyph, and a ghost ellipsis.

RIBBON, 36px, background #fafafa, hairline bottom border, a single dense row of controls at 12px:

- A tab strip on the left — "File", "Home", "Insert", "Layout", "References", "Review", "View" — with "Home" active as near-black medium and the rest muted.
- To the right of the tabs sit the formatting controls separated by 16px-tall vertical hairlines: a font-family bordered select showing "Noto Sans", a font-size bordered select showing "11", then ghost icon toggles for bold, italic, underline, strikethrough (the bold one active with a #f5f5f5 fill), then a text-color glyph with a thin black underline swatch and a highlight glyph, then alignment toggles left/centre/right/justify with left active, then list toggles bullet and numbered, then indent and outdent glyphs.

DOCUMENT STAGE, fills the remaining height, background #f5f5f5, scrolling, with the page centred: a white A4-proportioned page with a hairline edge and a very subtle shadow, roughly 800px wide, holding realistic business-document content — a right-aligned small header line "Acme Core — Confidential", a centred bold heading "VENDOR TERMS AND CONDITIONS", a subheading "Effective 1 April 2026", then numbered sections "1. Scope of services", "2. Payment terms", "3. Confidentiality" each with two or three paragraphs of real justified body copy at document scale, a small three-column table showing payment milestones with tabular figures, and a signature block at the bottom with two ruled lines. One paragraph in section 2 has a live collaborator selection over it — a cyan-blue text highlight at 20% opacity with a 2px cyan caret at its end carrying a tiny cyan name flag reading "Mai Tran". A second caret in a neutral dark gray with a flag reading "Le Hoang" sits in section 3.
A right-margin comment thread is anchored beside section 2: a 240px white card with a hairline ring, 12px padding, holding a 20px avatar, "Mai Tran" at 12px medium, a 10px muted "14 minutes ago", a 12px comment body "Can we push net-30 to net-45 here?", and below it a compact reply input placeholder "Reply" plus tiny ghost "Resolve" text button.

BOTTOM STATUS BAR, 28px, white, hairline top border, 10px muted type with tabular figures: on the left "Page 2 of 7", "1,482 words", "English (US)"; on the right a zoom control — a minus glyph, "100%", a plus glyph — then a fit-page glyph, then a small cyan dot with the text "3 people editing".

---

## 10. Public share link — anonymous view

Screen name: "Public share link — anonymous view". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration, no marketing hero.

What an anonymous visitor holding a share link sees — a shared folder. There is NO left rail and no app navigation, because the visitor has no account.

TOP BAR, 48px, white, hairline bottom border:

- Left: a small 20px square cyan-blue product mark with the letters "pp" in white, then "pp-hub" at 14px medium, then a 16px vertical hairline, then a 10px uppercase muted label "SHARED WITH YOU".
- Right: a ghost "Sign in" button and an outline "Sign in to save a copy" button, both 28px tall.

BANNER STRIP, 32px, background #f5f5f5, hairline bottom border, 12px type: a 14px muted info glyph, then "Mai Tran shared this folder with you · view and download only", then on the right a muted 10px tabular line "Link expires Mar 17, 2026".

CONTENT, 16px padding:

- A header block: a 20px outline folder glyph beside "Brand assets" at 20px semibold with tight tracking, then a 12px muted line "14 files · 248 MB · shared Mar 10, 2026", then a row of two buttons — a solid cyan-blue "Download all" 28px tall with a download glyph, and an outline "Copy link" button. On the right of the header row, a bordered grid/list segmented toggle with the list half active.
- A breadcrumb row at 12px: "Brand assets / Logos" with the last crumb near-black.
- A dense 12px table with a 40px medium muted header row and columns Name, Modified, Size, and a trailing action column holding only Preview and Download ghost icon buttons — no share, no rename, no delete anywhere on this screen. Twelve 36px rows with hairline bottom rules and distinct muted file-type glyphs:
  "Wordmarks" folder · Mar 10 · 6 items; "Icon set" folder · Mar 10 · 22 items;
  "logo-primary.svg" · Mar 10 · 18 KB; "logo-mono.svg" · Mar 10 · 16 KB; "logo-primary@2x.png" · Mar 10 · 284 KB; "logo-on-dark.png" · Mar 10 · 302 KB; "brand-guide.pdf" · Mar 12 · 8.1 MB; "color-tokens.json" · Mar 09 · 6 KB; "type-specimen.pdf" · Mar 08 · 2.2 MB; "photography-brief.docx" · Mar 07 · 96 KB; "packshot-hero.jpg" · Mar 05 · 4.8 MB; "motion-loop.mp4" · Mar 04 · 184 MB.
  Sizes use tabular figures and are right-shouldered. One row, "brand-guide.pdf", is hovered with a #fafafa fill and shows its two ghost action buttons clearly.
- Beneath the table, a hairline-bordered rounded-lg panel, 12px padding, with a 16px muted shield glyph, a 12px medium line "You are viewing a shared link", and a 12px muted line "Anyone with this link can view and download these files. You cannot edit, upload, or see who else has access. Ask Mai Tran for an account to collaborate."

FOOTER, 32px, hairline top border, 10px muted type: on the left "Served by pp-hub · self-hosted at hub.acme.dev"; on the right ghost text links "Report this link" and "Privacy".

---

## 11. Sign in & create space

Screen name: "Sign in & create space". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration, no marketing hero, no colored panel.

A single desktop screen split into two specimens side by side with a vertical hairline between them, each filling half the viewport width and the full height, each with a small uppercase 11px muted caption pinned to its top-left corner.

LEFT HALF, captioned "SIGN IN": content centred on both axes, stacked with a 16px gap, capped at 320px wide.

- A 32px square cyan-blue product mark with rounded corners holding the letters "pp" in white.
- "Sign in to pp-hub" at 20px semibold with tight tracking.
- A 12px muted line "Use your organisation account. pp-hub does not store your password."
- A solid cyan-blue button, 32px tall, full width, reading "Continue with Logto" with a small shield glyph.
- A hairline rule with the muted 10px word "OR" centred on it, breaking the rule on both sides.
- An outline button, 32px tall, full width, "Continue with another provider" with a key glyph.
- A hairline-bordered rounded-lg panel, 12px padding, holding an uppercase 10px muted label "SERVER" and a 12px line "hub.acme.dev" in tabular figures with a small cyan dot before it and a tiny ghost "Change" text button on the right.
- A 10px muted footer line "Self-hosted · v0.9.2 · Signed releases" and beneath it a red-tinted 12px inline error row with a 14px alert glyph reading "That session expired. Sign in again." sitting in a faint red-tinted rounded rectangle.

RIGHT HALF, captioned "CREATE A SPACE": a first-run dialog specimen centred on both axes — a white panel 420px wide, 14px rounded corners, hairline ring, 16px padding, 12px gaps.

- Header: a 14px medium title "Create a space" and a 12px muted line "Spaces are isolated file areas with their own members, permissions, and storage."
- A form: a 12px medium label "Name" over a 28px recessed translucent input containing the typed value "Marketing" with a caret; a 12px medium label "Slug" over a 28px recessed input whose left edge holds a muted non-editable prefix "hub.acme.dev/d/" and the value "marketing" — with a 10px muted helper line "Lowercase letters, numbers, and dashes." beneath; a 12px medium label "Description" over a recessed textarea two lines tall with the placeholder "What lives in this space?"; a 12px medium label "Default member role" over a three-way segmented control, one object with collapsed inner seams, reading "Viewer / Editor / Owner" with "Viewer" active in a #f5f5f5 fill; and a 12px medium label "Storage limit" over a row of a 28px recessed number input showing "50" in tabular figures and an adjacent bordered unit select showing "GB", with a 10px muted line "Leave blank to share the server pool."
- A hairline rule, then an uppercase 10px muted label "INVITE NOW (OPTIONAL)" over a recessed input holding two recipient chips, "mai@acme.dev" and "le@acme.dev", each with an ×, followed by a caret and the placeholder "Add emails".
- Footer: a hairline top rule with a right-aligned outline "Cancel" and solid cyan-blue "Create space" button pair, both 28px tall, and a muted 10px line on the left "You will be the owner."

---

## 12. Home — recent & shared with me

Screen name: "Home — recent & shared with me". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration, no large hero, no colored panels.

A landing surface a user sees on opening pp-hub — a working dashboard, not a welcome page.

The standard shell: fixed 256px left rail on #fafafa with hairline right border holding the space switcher "Acme Core / owner", a search field with a ⌘K hint, an uppercase QUICK ACCESS group where "Recent" is the ACTIVE row with a filled light-gray background, an uppercase MY DRIVE folder tree, an uppercase SPACES group, and a pinned bottom storage meter plus user row.

MAIN COLUMN:

- A 40px header with hairline bottom border: "Home" at 12px near-black medium on the left, and right-side ghost icon buttons for info, notifications with a small cyan dot, and theme.
- Content area, 16px padding, scrolling, bands separated by 24px.

BAND 1 — "CONTINUE WHERE YOU LEFT OFF": an uppercase 11px muted label, then a single row of five compact horizontal cards on a grid with a 12px gutter. Each card is 10px rounded with a hairline ring, 12px padding, and lays out as a 32px file-type thumbnail on the left beside a text block: a 12px medium truncated name, a 10px muted line "space · relative time". Cards: "Vendor terms.docx" / "Acme Core · edited 2 min ago" — this one carries a tiny cyan dot and a 10px cyan "Editing now" label instead of the time; "Q1 forecast.xlsx" / "Acme Core · opened 18 min ago"; "brand-guide.pdf" / "Acme Core · viewed 2 hours ago"; "Campaign brief.docx" / "Marketing · edited yesterday"; "walkthrough.mp4" / "Acme Core · viewed Mar 11".

BAND 2 — a two-column layout with a 24px gap, the left column flex-2 and the right flex-1.
LEFT COLUMN, uppercase 11px muted label "RECENT" with a small ghost "View all" text button on the right. Below it a dense 12px table with a 40px muted medium header row and columns Name, Space, Action, Modified, Size. Ten 36px rows with hairline bottom rules, distinct muted file-type glyphs, and the Action column holding small muted 10px verbs — "You edited", "Mai Tran edited", "You uploaded", "Le Hoang restored v3", "You downloaded", "Mai Tran shared", "You renamed", "You uploaded", "Le Hoang commented", "You moved". Rows use realistic names across spaces: "Vendor terms.docx / Acme Core / Today 14:08 / 96 KB"; "Q1 forecast.xlsx / Acme Core / Today 13:50 / 1.2 MB"; "Scan_0042.png / Acme Core / Today 11:12 / 2.4 MB"; "Board deck.pptx / Acme Core / Yesterday 17:31 / 8.7 MB"; "brand-guide.pdf / Acme Core / Yesterday 16:02 / 8.1 MB"; "Campaign brief.docx / Marketing / Yesterday 09:44 / 112 KB"; "budget.csv / Acme Core / Mar 12 / 61 KB"; "packshot-hero.jpg / Marketing / Mar 12 / 4.8 MB"; "notes.md / Research / Mar 11 / 4 KB"; "archive-2025.zip / Acme Core / Mar 10 / 1.4 GB". Dates and sizes use tabular figures.
RIGHT COLUMN, stacked with a 24px gap:

- An uppercase 11px muted label "SHARED WITH ME" with a muted 10px count "7". Below it five 44px rows with hairline bottom rules: each a 24px file-type glyph, a 12px medium truncated name, a 10px muted line "shared by Name · relative time", and a right-aligned 10px uppercase muted permission chip. Rows: "Partner-MSA.pdf / Mai Tran · 2 days ago / VIEW"; "Roadmap 2026.xlsx / Le Hoang · 4 days ago / EDIT"; "Design tokens.json / Mai Tran · Mar 09 / VIEW"; "Press kit (folder) / Pham Anh · Mar 07 / DOWNLOAD"; "Interview notes.docx / Tran Quoc · Mar 02 / EDIT".
- An uppercase 11px muted label "STORAGE" over a hairline-bordered rounded-lg panel, 12px padding: a 12px line "38.4 GB of 60 GB used" in tabular figures with a right-aligned muted 10px "64%", then a 6px-tall stacked hairline bar whose segments are shades of neutral gray with only the largest segment in cyan-blue, then a compact legend of four 12px rows, each a 8px square swatch, a category name, and a right-aligned tabular size: "Video 18.2 GB", "Documents 9.6 GB", "Images 7.1 GB", "Other 3.5 GB". A 10px muted footer line "Trash holds 2.1 GB · empty it to reclaim space" with a small cyan-blue link-style "Empty trash" button.
- An uppercase 11px muted label "ACTIVITY" over a compact timeline of five entries on a hairline vertical rule with small dots, each a 12px line with an inline 16px avatar and a 10px muted timestamp beneath: "Mai Tran uploaded 3 files to Design · 12 min ago"; "Le Hoang restored Q1 forecast.xlsx to v3 · 1 hour ago"; "You invited khanh@acme.dev as Editor · 3 hours ago"; "Mai Tran created a link share for brand-guide.pdf · Yesterday"; "Pham Anh joined Acme Core · Mar 10".

---

## 13. Drive — dark mode (with context menu)

Screen name: "Drive — dark mode". Dense, achromatic, engineered, in the DARK theme. Page ground is near-black #0a0a0a; panels and the left rail LIFT above it to #171717; every rule and border is white at 10% opacity so it stays hairline-thin; body text is #fafafa; all metadata, timestamps, sizes, owners and section labels are #a1a1a1. The single cyan-blue accent #2980b9 is UNCHANGED from the light theme. Destructive red is #ff6467, always used as a 10% tint behind red text, never a solid fill. 12px body type, 10-11px uppercase muted labels, 28px controls. No gradients, no illustration, no second hue.

The same file browser as the light theme, rendered dark.

LEFT RAIL, fixed 256px, background #171717, hairline right border in white-at-10%, three bands:

- A space switcher with a 20px rounded-square cyan monogram "AC", "Acme Core" at 12px medium in #fafafa, a 10px "owner" beneath in #a1a1a1, and a chevron-up-down glyph.
- A scrolling middle band: a search field with a recessed translucent fill, a magnifier glyph, the placeholder "Search files" and a muted "⌘K" chip; an uppercase 11px "QUICK ACCESS" label in #a1a1a1 over ghost rows Recent, Starred, Shared with me, Trash; an uppercase "MY DRIVE" label over a folder tree — Documents expanded with children Contracts, Invoices, Reports, then Design, Engineering, Media, Archive — where "Invoices" is active with a filled #262626 background and #fafafa medium text; an uppercase "SPACES" label over Acme Core (owner), Marketing (editor), Research (viewer) with roles right-aligned in 10px uppercase #a1a1a1.
- A bottom band pinned by a hairline top border: an uppercase 10px "STORAGE" label, a 4px track in #262626 with a cyan-blue fill 62% across, "38.4 GB of 60 GB" in 10px #a1a1a1 tabular figures, then a user row with a 24px circular avatar carrying a cyan status dot ringed 2px in #171717, "Nguyen Bui" at 12px medium, "nguyen@acme.dev" at 10px muted, and an ellipsis glyph.

MAIN COLUMN on #0a0a0a:

- A 40px header with a hairline bottom border: breadcrumbs "Acme Core / Documents / Invoices" at 12px with the last crumb #fafafa and earlier crumbs #a1a1a1, and right-side ghost icon buttons — info, notifications with a small cyan dot, and a theme toggle showing a SUN glyph to indicate switching back to light.
- A 40px toolbar with a hairline bottom border: a solid cyan-blue "Upload" button with near-white text, an outline "New folder" button whose border is white-at-10%, an outline "New document" button with a caret, a vertical hairline, filter chips "Type", "Modified", "Owner", and on the right a "Name ↑" ghost sort button and a bordered two-icon grid/list segmented toggle with the list half active on a #262626 fill.
- A dense 12px table. A 40px header row in #a1a1a1 medium with a checkbox cell then Name, Modified, Size, Owner and a trailing action column. Fifteen 36px rows with hairline bottom rules in white-at-10%:
  Folders first with filled folder glyphs: "Q1 2026" · — · 12 items · Nguyen Bui; "Q4 2025" · — · 31 items · Nguyen Bui; "Receipts" · — · 8 items · Mai Tran.
  Then files with distinct muted outline type glyphs: "Invoice-2026-0114.pdf" · Today, 09:42 · 248 KB · Nguyen Bui; "Vendor terms.docx" · Yesterday, 17:03 · 96 KB · Mai Tran; "Q1 forecast.xlsx" · Yesterday, 11:20 · 1.2 MB · Le Hoang; "Board deck.pptx" · Mar 12 · 8.7 MB · Mai Tran; "Signed-contract-acme.pdf" · Mar 11 · 412 KB · Nguyen Bui; "Scan_0042.png" · Mar 10 · 2.4 MB · Le Hoang; "notes.md" · Mar 9 · 4 KB · Nguyen Bui; "budget.csv" · Mar 8 · 61 KB · Mai Tran; "walkthrough.mp4" · Mar 5 · 184 MB · Le Hoang; "logo-final.svg" · Mar 4 · 18 KB · Mai Tran; "archive-2025.zip" · Mar 1 · 1.4 GB · Nguyen Bui; "README.rtf" · Feb 27 · 9 KB · Nguyen Bui.
  Two rows are selected — "Vendor terms.docx" and "Q1 forecast.xlsx" — with cyan-checked checkboxes and a #262626 row fill. The row "Board deck.pptx" is hovered with a #1f1f1f fill and reveals trailing ghost icon actions: star, share, download, ellipsis. A small cyan "Shared" badge follows the name on "Signed-contract-acme.pdf". Sizes and dates use tabular figures, right-shouldered against the action column.
- An open context menu floating over the table, anchored to the "Board deck.pptx" row: a #171717 panel, 10px rounded, 4px padding, a hairline white-at-10% ring PLUS a real soft shadow — menus are the only surface that carries one. Its items are 28px tall, 8px horizontal padding, 6px rounded, 12px #fafafa text with a 16px leading glyph and a right-aligned 10px #737373 shortcut: "Open" (↵), "Open in new tab" (⌘↵), "Preview" (Space), then a hairline separator, "Download" (⌘D), "Share" (⌘⇧S), "Copy link", then a separator, "Rename" (F2), "Move to…", "Make a copy", "Version history", then a separator, and finally "Move to trash" (⌫) rendered in #ff6467 with a red-tinted 10% background on hover. One item, "Share", is focused with a #262626 fill.

---

## 14. Trash & admin console

Screen name: "Trash & admin console". Dense, achromatic, engineered. Pure white ground, near-black text, one flat cyan-blue accent (#2980b9), hairline #e5e5e5 rules, 12px body type, 10-11px uppercase muted labels, 28px controls. Destructive red #e7000b always rendered as a 10% tint behind red text, never a solid red button. No gradients, no illustration.

A single desktop screen showing two administrative surfaces stacked vertically with a 24px gap and 16px page padding, each a white panel with a hairline ring and 10px rounded corners, each preceded by a small uppercase 11px muted caption.

PANEL 1, captioned "TRASH":

- A 40px header row with a hairline bottom border: "Trash" at 14px medium with a 10px muted line beneath "18 items · 2.1 GB · items are purged automatically after 30 days"; on the right an outline "Restore selected" button 28px tall with a rotate-left glyph, and a red-tinted "Empty trash" button — red text on a 10% red fill with a trash glyph.
- A 32px filter row: a 28px recessed search input "Filter deleted items", bordered selects "Deleted by: Anyone" and "Deleted: Any time", and a right-aligned muted 10px "Showing 18 of 18".
- A dense 12px table, 40px muted medium header row with a checkbox cell then columns Name, Original location, Deleted by, Deleted, Purges in, Size, and a trailing action column. Eight 36px rows with hairline bottom rules and distinct muted file-type glyphs. The Purges in column uses tabular figures and its values turn red when under three days. The action column holds two tiny ghost text buttons, "Restore" and a red-tinted "Delete forever".
  Rows: "Old-invoice-template.docx" · Acme Core / Documents · Nguyen Bui · Mar 12 · 28 days · 84 KB.
  "Q3 2025 (folder)" · Acme Core / Documents / Archive · Mai Tran · Mar 11 · 27 days · 412 MB — with a muted 10px "14 items" note under the name.
  "screenshot-draft.png" · Acme Core / Design · Le Hoang · Mar 09 · 25 days · 1.8 MB.
  "budget-v1.csv" · Acme Core / Finance · Nguyen Bui · Mar 02 · 18 days · 44 KB.
  "render-test.mp4" · Marketing / Media · Mai Tran · Feb 24 · 10 days · 1.2 GB.
  "meeting-notes-old.md" · Research · Tran Quoc · Feb 19 · 5 days · 6 KB.
  "logo-draft-v2.svg" · Acme Core / Design · Mai Tran · Feb 17 · 2 days · 22 KB — "2 days" rendered in red.
  "temp-export.zip" · Acme Core · Nguyen Bui · Feb 16 · 1 day · 318 MB — "1 day" rendered in red, and the whole row carries a faint 10% red tint.
  Two rows, "budget-v1.csv" and "meeting-notes-old.md", are selected with cyan-checked checkboxes and a #f5f5f5 fill.

PANEL 2, captioned "ADMIN — USERS & SERVER":

- A 40px header with a hairline bottom border: "Server administration" at 14px medium with a 10px muted line "hub.acme.dev · pp-hub v0.9.2 · uptime 41 days"; on the right a solid cyan-blue "Invite user" button and an outline "Server settings" button.
- A row of five compact stat tiles on a grid with a 12px gutter, each a hairline-bordered rounded-lg panel with 12px padding holding an uppercase 10px muted label over a 20px semibold tabular value and a 10px muted delta line: "USERS" / "42" / "+3 this month"; "SPACES" / "9" / "2 archived"; "FILES" / "128,410" / "+1,204 this week"; "STORAGE USED" / "1.8 TB" / "of 4.0 TB pool"; "PREVIEW QUEUE" / "17" / "2 failed" where "2 failed" is in red.
- An uppercase 11px muted label "USERS" over a dense 12px table with a 40px muted medium header row and columns User, Role, Spaces, Storage, Last seen, Status, and a trailing action column. Seven 36px rows with hairline rules. The User cell holds a 24px avatar, a 12px medium name and a 10px muted email. Role is a compact bordered select. Storage shows a value plus a 3px-tall inline hairline meter beneath it. Status is a 10px uppercase chip — muted "ACTIVE", or a red-tinted "SUSPENDED", or a cyan-tinted "INVITED".
  Rows: "Nguyen Bui / nguyen@acme.dev" · Admin · 3 · 182 GB · 2 min ago · ACTIVE, with a "You" chip.
  "Mai Tran / mai@acme.dev" · Member · 3 · 241 GB · 18 min ago · ACTIVE.
  "Le Hoang / le@acme.dev" · Member · 2 · 96 GB · 3 hours ago · ACTIVE.
  "Pham Anh / anh@acme.dev" · Member · 1 · 12 GB · Yesterday · ACTIVE.
  "Hoang Yen / yen@partner.io" · Guest · 1 · 0 GB · Mar 09 · ACTIVE, with an "External" chip.
  "Vu Minh / minh@acme.dev" · Member · 1 · 4 GB · Feb 28 · SUSPENDED, the row carrying a faint 10% red tint.
  "khanh@acme.dev" · Member · 0 · — · — · INVITED, its avatar a light-gray circle with an envelope glyph and the email in muted italic.

---

# Apps & kanban (M44–M46)

Ten screens for the extensions feature, generated into project 1 with the
`pp-hub` design system. Local copies (PNG + HTML) go in `.stitch/designs/`, which is git-ignored; re-download any of them with `get_screen`.
Fetch any of them with `get_screen` and `name = projects/11969748414038004354/screens/<id>`.

| #   | Screen                                                           | Screen id                          | File                                       |
| :-- | :--------------------------------------------------------------- | :--------------------------------- | :----------------------------------------- |
| 01  | Board editor — columns, cards, WIP limit, drag and drop          | `0a2d6708ce9a4f04af3eb03cd0d031f8` | `designs/01-kanban-board.*`                |
| 02  | Card dialog — assignees, labels, linked items, discussion        | `a166f8954ea246dbaffa26bc6f1afd85` | `designs/02-kanban-card-dialog.*`          |
| 03  | Admin › Apps — instance switch                                   | `3a9e4d6b60034dab9de1f923246279ad` | `designs/03-admin-apps.*`                  |
| 04  | Space settings › Apps — per-space switch                         | `a7631757e3ff47d58b963bef0e8a6efe` | `designs/04-space-settings-apps.*`         |
| 05  | New menu entry and the naming dialog                             | `14287242fe8c401694f22cd30d42901f` | `designs/05-new-kanban-board.*`            |
| 06  | States — view-only, app turned off, app not in this installation | `bff82edfdef4407499d8d40a28e01fd3` | `designs/06-kanban-states.*`               |
| 07  | Notification bell with an assignment                             | `bbac2c69b2814692ba0638d5ba687a67` | `designs/07-kanban-notification.*`         |
| 08  | Board editor, dark mode                                          | `5e4dbd817a2c46a78e2dbc94291eb7d7` | `designs/08-kanban-board-dark.*`           |
| 09  | Delete-column and Manage-labels dialogs                          | `1d023c9de0e2493f9345fb0047abd4b0` | `designs/09-kanban-column-label-dialogs.*` |
| 10  | Empty board and the inline card composer                         | `83dfe73478024a1599b87798c586f874` | `designs/10-kanban-empty-board.*`          |

## Things to know when re-running

- **Screens made through the MCP API do not show on the project canvas.**
  `list_screens` and `get_project.screenInstances` omit them (a fresh project
  behaves the same), but `get_screen` returns them. This is the attach problem
  already noted above.
- **The 30 s MCP request timeout is shorter than a generation (about 70–225 s).**
  `generate_screen_from_text` through the harness times out and the outcome is
  unknown. `.mcp.json` now has `"timeout": 600000` on the `stitch` server (the
  file is git-ignored); it takes effect after the harness restarts. Until then
  the same call can be made straight over HTTP: `initialize`, then
  `tools/call` against `https://stitch.googleapis.com/mcp` with the
  `X-Goog-Api-Key` header, no client-side deadline.
- The prompts describe layout, content and states only; colour, type and shape
  come from the design system, so they carry no hex codes.
- Differences from the build the designs invite: Stitch drew a breadcrumb, a
  space badge, card keys (`CORE-112`), "Request edit access", "Used in 4 of 6
  spaces", a card progress ratio and a `⌘F` hint. None of those exist in the app.
  What the app has is in `docs/architecture/extensions.md`.
