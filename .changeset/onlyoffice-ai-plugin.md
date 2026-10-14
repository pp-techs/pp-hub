---
"@pp-hub/release": patch
---

OnlyOffice's AI plugin is not in the 10.0.0.1 image's default plugin list or in its in-editor Plugin Manager, so the editor had no AI tab and users could not add one. The `onlyoffice` service now installs it on start (needs outbound access to `onlyoffice.github.io` on first start); nothing is sent to an AI provider until a user sets one up with their own API key in the AI tab. `PP_ONLYOFFICE_AI=false` removes it. A hand-written compose needs the `ENABLE_AI_PLUGIN` variable and the three lines before `exec` from `compose.prod.yml`.
