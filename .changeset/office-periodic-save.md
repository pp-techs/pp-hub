---
"@pp-hub/release": patch
---

Office files no longer lose everything typed since they were opened when a redeploy or crash replaces the editor. The server now asks the Document Server to save every open document each 5 minutes (`PP_OFFICE_AUTOSAVE_INTERVAL`, `off` to disable), and in `compose.prod.yml` the Document Server is now stopped first, with time to finish, before the server. Each save that finds edits adds a version to the file. The server needs `PP_ONLYOFFICE_JWT_SECRET` and `PP_ONLYOFFICE_INTERNAL_URL`, which `compose.prod.yml` now passes to it; a hand-written compose must do the same.
