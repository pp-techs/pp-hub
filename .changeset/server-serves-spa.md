---
"@pp-hub/release": minor
---

**Breaking for operators:** the nginx `web` container and the `ghcr.io/pp-techs/pp-hub/web` image are gone. The `server` image now serves the built SPA itself on the same listener as the API (new `PP_WEB_DIR`, preset to `/srv/web` in the image; unset keeps the API-only development mode), with the security headers, CSP, gzip and cache headers nginx used to add. Re-point your Dokploy/Traefik/Caddy domain from `web:80` to `server:8080`, and use `server` instead of `web` in `docker compose logs`/`restart`. WOPI moved to its own listener, `PP_WOPI_ADDR` (default `:8081`), which is not on the public port: set `PP_WOPI_SERVER_URL` to `http://server:8081` in any hand-written environment (`compose.prod.yml` already does). `PP_ONLYOFFICE_URL` is now read only by `server`. Docker builds of `server` and `worker` use the repository root as build context (`docker build -f apps/server/Dockerfile --target server .`).
