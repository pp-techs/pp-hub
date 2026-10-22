---
"@pp-hub/release": minor
---

Apps can now be uploaded. In Admin › Apps, **Upload app** installs an app from a `.zip` or a folder — no rebuild, no restart — and **Upload new version…**, **Backend…** and **Remove…** manage it afterwards. An uploaded app runs in a sandboxed frame with no access to the session: it reads and writes only the item it is opened on, with the access of whoever opens it, through the record store, live updates, comments and notifications pp-hub provides. An app that needs its own server names a backend; pp-hub forwards its calls there with a short-lived signed token (generate the secret under **Backend…**). Like every app, an uploaded one arrives switched off.

Write one with `bun run apps init <dir>`, package it with `bun run apps pack <dir>`, and see `examples/apps/tasks` and `packages/app-sdk`. Migration `0031` adds the package and record tables and is applied automatically. App files are served under `/x/` — from `PP_ARTIFACT_URL`'s host when it is set — and an app's backend calls back under `/api/apps/`; a proxy in front of pp-hub must pass both through, and treat `/api/v1/apps/*/events` like the notification stream (no buffering).
