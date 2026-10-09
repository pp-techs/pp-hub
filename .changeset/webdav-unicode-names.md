---
"@pp-hub/release": patch
---

**Fix: files on a WebDAV mount with accented names could not be opened from Finder.** Finder and macOS apps ask for a name like `Bùi Đức Nhật` in decomposed Unicode (NFD) while pp-hub stores and lists it composed (NFC), so listing a folder worked but opening a file in it failed with "can't be found". WebDAV paths now match in either form.
