# Directory Structure

Monorepo managed with bun workspaces + Vite+ (`vp`). Workspaces: `packages/*`, `apps/*`, `tools/*`.

```
pp-hub/
├── apps/
│   ├── web/                     # React SPA (TanStack Router + Query, shadcn/ui): login, drive, share landing
│   └── server/                  # Go backend — own go.mod (M1 done)
│       ├── cmd/server/          # entry point: config, PG pool, migrate, OIDC wait, http.Server
│       ├── internal/
│       │   ├── auth/            # ✅ OIDC login/callback/logout, HMAC session cookies (go-oidc)
│       │   ├── config/          # ✅ env-based config
│       │   ├── httpapi/         # ✅ chi routes (/healthz, /auth/*, /api/v1/me), RFC 7807 errors
│       │   ├── store/           # ✅ sqlc queries, embedded migrations runner
│       │   ├── spaces/          # space CRUD, member roles (M2)
│       │   ├── fs/              # file tree, versioning, move/copy/rename (M2)
│       │   ├── blob/            # CAS Store interface + local impl (M2; S3 impl later)
│       │   ├── share/           # share links, direct grants (M3)
│       │   ├── wopi/            # WOPI host endpoints, access tokens (M4)
│       │   └── preview/         # job workers: thumbnail, pdf, video, text (M5)
│       └── sqlc.yaml
├── packages/
│   ├── utils/                   # existing shared utils
│   ├── ui/                      # @workspace/ui — shadcn/ui preset components + design tokens (globals.css)
├── tools/
│   └── compose/                 # ✅ docker-compose — postgres, keycloak (+realm import), server; onlyoffice lands in M4
├── docs/                        # this documentation
├── package.json                 # bun workspaces, Vite+ catalog
├── vite.config.ts
└── tsconfig.json
```

## Entry Points

- Web dev: `vp run web#dev` (root `vp run dev`); Vite proxies `/api`, `/auth`, `/s` → `localhost:8080`.
- Server local: `go run ./cmd/server` from `apps/server` (needs Postgres + Keycloak from compose).
- Full stack: `docker compose -f tools/compose/docker-compose.yml up --build` — Keycloak on :9080 (realm `pphub`, user `dev`/`dev`), server on :8080, then open `http://localhost:5173`.
- sqlc codegen: `go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate` in `apps/server`.

## Notes

- The Go module lives inside the JS monorepo but is independent of bun workspaces; root scripts may add convenience wrappers (e.g., `vp run server#dev`) via `vite.config.ts` tasks.
- `openapi.yaml` (planned at `apps/server/openapi.yaml` or `packages/api-client/`) is the FE↔BE contract; both sides generate from it.
