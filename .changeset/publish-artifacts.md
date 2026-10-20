---
"@pp-hub/release": minor
---

Publish an `.html` file or a folder as a public mini web site, like an AI chat's artifact link: `Publish…` takes a snapshot, gives it a stable `/p/<link>` URL, and Republish updates it in place. Scripts run sandboxed with no network access; styles, scripts and fonts may come from an admin-chosen CDN allowlist. A publication can carry a password and an expiry, and agents can publish over MCP (`publish_artifact`). Operators enable it by switching on Admin › Settings › Sharing › Publishing; it is off by default. No second domain is needed: published pages are served under `/a/` on the app's own address, guarded by the browser's CSP sandbox alone. Before letting untrusted people publish, set `PP_ARTIFACT_URL` to a separate domain routed to `server` so pages run on an origin with no session; the admin page warns until it is set.
