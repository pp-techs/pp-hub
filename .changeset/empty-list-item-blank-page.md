---
"@pp-hub/release": patch
---

A markdown document with an empty list item (a numbered `1.` with no text, or an item that opens with a sub-list) no longer opens as a blank page. The server now gives such items an empty first paragraph, and an editor that still cannot read a document's content shows an error with a retry instead of an empty editor that could overwrite the document.
