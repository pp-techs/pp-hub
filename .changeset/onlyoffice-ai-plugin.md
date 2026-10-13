---
"@pp-hub/release": patch
---

OnlyOffice's AI plugin is not in the 10.0.0.1 image's default plugin list, so the editor has no AI tab unless it is added back. Set `PP_ONLYOFFICE_AI=true` to have the `onlyoffice` service install it on start (off by default: it sends document text to the provider each user configures, and needs outbound access to `onlyoffice.github.io` on first start). A hand-written compose needs the `ENABLE_AI_PLUGIN` variable and the three lines before `exec` from `compose.prod.yml`.
