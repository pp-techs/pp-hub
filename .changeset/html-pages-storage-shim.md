---
"@pp-hub/release": patch
---

HTML pages (published sites and the file preview) that read `localStorage` or `sessionStorage` at load, such as slide decks with arrow-key navigation, now work. The sandbox that isolates them makes those reads throw, which stopped the page's script; pp-hub now gives such a page an in-memory store for the visit. Nothing persists between visits.
