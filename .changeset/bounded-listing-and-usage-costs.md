---
"@pp-hub/release": patch
---

Large drives stay fast. Folders with thousands of items render only the rows on screen, folder listings resolve access once per folder instead of once per item, and Home, Recent and the space list no longer slow down with history. Storage usage is now kept current by the database instead of being re-summed on every save, and it now drops when files are purged from Trash; the first start after upgrading recomputes every space's figure, which may lower it. Finished preview jobs older than 7 days are deleted automatically (failed jobs are kept). Thumbnails are no longer generated for images over about 67 megapixels or SVG files over 8 MiB.

The first start after upgrading builds new database indexes; writes to the affected tables wait until that finishes.
