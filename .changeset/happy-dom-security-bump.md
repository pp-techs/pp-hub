---
"@pp-hub/release": patch
---

Bumped `happy-dom` to 21.0.4 in the web workspace to fix CVE-2026-115895 (GHSA-834v-8h66-3h79), an incomplete fix for CVE-2026-33943 that allowed host code execution through an unsanitized ESM import alias. Development/test-only dependency; no production code affected.
