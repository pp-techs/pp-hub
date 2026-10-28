---
"@pp-hub/release": patch
---

Restoring an older version of an office document now terminates any active OnlyOffice co-authoring session and clears its WOPI lock so the restore succeeds immediately rather than returning a 409 Conflict. Stale WOPI locks left behind when browser tabs close without an unload event no longer block restore for 30 minutes, and the web file view now automatically refreshes with a fresh editor session upon restoring a version.
