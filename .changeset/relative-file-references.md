---
"@pp-hub/release": minor
---

Markdown documents now show files beside them by relative path, as a README in a repository does: `![](images/a.png)`, `[spec](../spec.pdf)`, and relative video or file blocks render from the drive, and relative links open the page or file they name. This covers documents written by agents through a WebDAV mount, `docs/` folders uploaded from a repository, and unpacked exports. The markdown keeps the path. A writer can **Embed in document** to keep the document's own copy of the file instead.

Access change: readers of a document, share-link visitors included, now see the files it names inside the folder it sits in, when the author of the version being read can download those files. A document at the top of a space reaches nothing this way, and someone who can edit only that one document cannot use it to read the files around it.

Document export brings those files into `assets/`.
