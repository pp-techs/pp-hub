---
"@pp-hub/release": minor
---

Search now looks inside PDFs and office documents (`.docx`, `.xlsx`, `.pptx`, `.doc`, `.xls`, `.ppt` and the OpenDocument formats), accents ignored, as it already did for markdown and plain text. The text is read by the worker, so it needs `cmd/worker` running with OnlyOffice reachable and `PP_SESSION_KEY` matching the server's, as for office thumbnails; the server queues every existing file at its next start and the worker drains them, so no operator step is needed beyond having a worker. Without one those files stay matched by name only. Scanned PDFs are not OCR'd.
