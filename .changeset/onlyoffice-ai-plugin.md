---
"@pp-hub/release": patch
---

OnlyOffice's AI plugin is not in the 10.0.0.1 image's default plugin list or in its in-editor Plugin Manager, so the editor had no AI tab and users could not add one. The `onlyoffice` service now installs it on start (needs outbound access to `onlyoffice.github.io` on first start); nothing is sent to an AI provider until a user sets one up with their own API key in the AI tab. A hand-written compose needs the `grep … sed …` step from the `entrypoint` in `compose.prod.yml`.
