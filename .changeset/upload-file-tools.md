---
"@pp-hub/release": minor
---

MCP agents can now put files in the drive: `upload_file` uploads a new file of any format — images, PDFs, office documents — under a folder or at the top of a space, and `update_file` replaces an existing file's bytes as a new version, so what `download_file` reads can be written back. Both cap at 8 MiB and keep the drive's name, permission, quota and editor-lock rules.
