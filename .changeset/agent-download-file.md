---
"@pp-hub/release": minor
---

Agents connected over MCP can now download files: the new `download_file` tool returns a file's bytes (base64, up to 8 MiB) — office documents, PDFs, images — for anyone who has download permission on it. It is read-only; writing binary files back is not supported.
