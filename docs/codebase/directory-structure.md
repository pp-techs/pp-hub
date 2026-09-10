# Directory Structure

Monorepo managed with bun workspaces + Vite+ (`vp`). Workspaces: `packages/*`, `apps/*`, `tools/*`.

pp-hub/
├── apps/
│ ├── web/ # React SPA
│ │ └── src/
│ │ ├── lib/
│ │ │ ├── api/ # ky client, types, endpoints (per-resource)
│ │ │ ├── queries/ # TanStack Query hooks (per-resource)
│ │ │ └── ui-store.ts # zustand ephemeral UI state
│ │ ├── components/ # UI panels
│ │ └── routes/ # TanStack Router file-based routes
│ └── server/ # Go backend — own go.mod
│ ├── cmd/server/ # entry point: config, PG pool, migrate, OIDC wait, http.Server
│ ├── internal/
│ │ ├── auth/ # ✅ OIDC login/callback/logout, HMAC session cookies (go-oidc)
│ │ ├── config/ # ✅ env-based config
│ │ ├── httpapi/ # ✅ chi routes (/healthz, /auth/*, /api/v1/me), RFC 7807 errors
│ │ ├── store/ # ✅ sqlc queries, embedded migrations runner
│ │ ├── spaces/ # space CRUD, member roles (M2)
│ │ ├── fs/ # file tree, versioning, move/copy/rename (M2)
│ │ ├── blob/ # CAS Store interface + local impl (M2; S3 impl later)
│ │ ├── share/ # share links, direct grants (M3)
│ │ ├── wopi/ # WOPI host endpoints, access tokens (M4)
│ │ └── preview/ # job workers: thumbnail, pdf, video, text (M5)
│ └── sqlc.yaml
├── packages/
│ ├── utils/ # existing shared utils
│ ├── ui/ # @workspace/ui — shadcn/ui preset components + design tokens (globals.css)
├── tools/
│ └── compose/ # ✅ docker-compose — postgres, Zitadel (+ its database), onlyoffice; server chạy local với `go run`
├── docs/ # this documentation
├── package.json # bun workspaces, Vite+ catalog

## Entry Points

- Web dev: `vp run web#dev` (root `vp run dev`); Vite proxies `/api`, `/auth`, `/s` → `localhost:8080`.
- Server local: `go run ./apps/server/cmd/server` (cần postgres + Zitadel từ compose).
- IdP dev: `docker compose -f tools/compose/docker-compose.yml up` — Zitadel ở :9080. Lần đầu: mở http://localhost:9080 làm theo wizard để tạo admin user, rồi tạo OIDC app. Xem [docs/architecture/zitadel-setup.md](docs/architecture/zitadel-setup.md).

## Notes

- The Go module lives inside the JS monorepo but is independent of bun workspaces; root scripts may add convenience wrappers (e.g., `vp run server#dev`) via `vite.config.ts` tasks.
- `openapi.yaml` (planned at `apps/server/openapi.yaml` or `packages/api-client/`) is the FE↔BE contract; both sides generate from it.
