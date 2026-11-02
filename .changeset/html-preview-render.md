---
"@pp-hub/release": minor
---

HTML files now preview as a rendered page instead of coloured source. The preview panel and the full-size viewer both show a Preview/Source toggle for `.html`/`.htm`/`.xhtml` files: Preview renders the file in a sandboxed `<iframe>` (scripts run, but the frame gets an opaque origin with no access to pp-hub's cookies or session), Source shows the highlighted markup as before. "Open" now also opens HTML files full-size, alongside PDFs, images, video and audio.
