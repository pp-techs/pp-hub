---
"@pp-hub/release": minor
---

**Breaking for operators:** the container images are renamed so they are recognisable outside this repository: `ghcr.io/pp-techs/pp-hub/server` is now `ghcr.io/pp-techs/pp-hub-server`, and `ghcr.io/pp-techs/pp-hub/worker` is now `ghcr.io/pp-techs/pp-hub-worker`. `deploy/compose/compose.prod.yml` already uses the new names, so a stack deployed from it needs nothing beyond pulling the new release. A hand-written compose file or platform application that names the old images must be edited, and `PP_IMAGE_TAG` has to be a release from this one on; the old packages are no longer updated.
