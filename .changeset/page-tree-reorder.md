---
"@pp-hub/release": minor
---

**Reorder, re-parent and trash sub-pages from the page tree.** Drag a row above or below another to reorder it, or onto the middle of a row to make it a sub-page of it; Alt+↑/↓ moves it among its siblings from the keyboard, and a row's trash button removes it with an Undo toast. The order is kept per parent (`PATCH /nodes/{id}/move` takes an optional `index`); pages never moved keep their creation order. Includes slash-editor 0.8.0 and migration `0026_page_order.sql`, applied automatically.
