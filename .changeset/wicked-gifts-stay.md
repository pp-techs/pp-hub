---
"@pp-hub/release": minor
---

Add MCP reads and conditional edits for UTF-8 text files, including HTML, CSS, JavaScript/TypeScript, JSON, XML, YAML, SVG and CSV, while preserving Markdown collaboration.

Add structured DOCX/XLSX/PPTX read/edit tools with schema discovery, permission and editor-lock checks, stale-version protection and atomic validated output. Deploy the new isolated `office-agent` image alongside the server; it uses pinned OfficeCLI over a private Unix socket without network, application secrets or blob mounts.

Correct invalid OOXML ordering and spreadsheet DPI metadata in bundled blank templates so newly created files support validated Office edits.
