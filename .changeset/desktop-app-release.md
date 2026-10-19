---
"@pp-hub/release": minor
---

Each release now attaches a desktop app (macOS arm64 and x64 `.dmg`, Windows x64 `.msi`) built with Pake around your deployment's URL. Set the repository variable `DESKTOP_APP_URL` to the address users open pp-hub at; the build fails until it is set. The apps are unsigned, so macOS and Windows warn on first launch.
