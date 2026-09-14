# Directory Structure

Monorepo managed with bun workspaces + Vite+ (`vp`). Workspaces: `packages/*`, `apps/*`, `tools/*` (declared in root `package.json`; `tools/` is reserved and currently empty — deploy lives at `deploy/`).

```
pp-hub/
├── apps/
│   ├── web/                        # React SPA
│   │   └── src/
│   │       ├── lib/
│   │       │   ├── api/            # ky client, types, endpoints, utils
│   │       │   ├── queries/        # TanStack Query hooks (per-resource)
│   │       │   ├── query-client.ts # shared QueryClient
│   │       │   └── ui-store.ts     # zustand ephemeral UI state
│   │       ├── components/         # app-level UI panels
│   │       │   ├── share-panel.tsx       # share dialog (direct + link)
│   │       │   ├── space-members-panel.tsx # space member management
│   │       │   ├── member-picker.tsx     # user search + member add
│   │       │   ├── user-avatar.tsx       # avatar with fallback
│   │       │   ├── confirm-dialog.tsx    # confirmation modal
│   │       │   └── ...                  # folder-tree, preview, nav, etc. (edit is a route, not a panel)
│   │       └── routes/             # TanStack Router file-based routes
│   │           ├── profile.tsx          # user profile page
│   │           ├── admin.users.tsx      # admin: user management
│   │           ├── edit/$nodeId.tsx     # full-page OnlyOffice editor (opened in a new tab)
│   │           └── drive/$spaceId.tsx   # space drive view
│   └── server/                     # Go backend — own go.mod
│       ├── cmd/server/             # entry point: config, PG pool, migrate, OIDC wait, http.Server
│       └── internal/
│           ├── auth/               # ✅ OIDC login/callback/logout, HMAC session cookies (go-oidc)
│           ├── config/             # ✅ env-based config
│           ├── httpapi/            # ✅ chi routes (/healthz, /auth/*, /api/v1/*), RFC 7807 errors
│           │   ├── users.go        # user profile CRUD + search endpoints
│           │   └── spaces_members.go # space members API (list/add/update/remove)
│           ├── store/              # ✅ sqlc queries, embedded migrations runner
│           ├── users/              # user service layer (profile, search)
│           ├── spaces/             # space CRUD, member roles
│           ├── fs/                 # file tree, versioning, move/copy/rename
│           ├── blob/               # CAS Store interface + local impl (S3 impl later)
│           ├── share/              # share links, direct grants
│           ├── wopi/               # WOPI host endpoints, access tokens
│           └── preview/            # job workers: thumbnail, pdf, video, text
├── packages/
│   ├── ui/                         # @workspace/ui — shadcn/ui preset components + design tokens (globals.css)
│   └── utils/                      # shared TS utils (template package, dist via vp pack)
├── deploy/
│   └── compose/                    # docker-compose variants + env templates + README (dev workflows)
├── docs/                           # this documentation
└── package.json                    # bun workspaces, Vite+ catalog
```

## Entry Points

- Web dev: `vp run web#dev` (root `vp run dev`); Vite proxies `/api`, `/auth`, `/s` → `localhost:8080`.
- Server local: `cp deploy/compose/server.env apps/server/.env` (fill `PP_OIDC_*` from Logto) then `go run apps/server/cmd/server` (needs postgres + IdP).
- IdP/dev stack: `docker compose -f deploy/compose/compose.pg-oo.yml up -d` — postgres + OnlyOffice at :9980; IdP is Logto Cloud (default) or self-host via `compose.logto.yml` (admin console at :3002). Full setup steps: [deploy/compose/README.md](../../deploy/compose/README.md).

## Notes

- The Go module lives inside the JS monorepo but is independent of bun workspaces.
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen is deferred until the API surface justifies it.
