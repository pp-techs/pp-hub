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
│   │       │   ├── quick-access.tsx      # rail entries for the Trash / Shared panels
│   │       │   ├── drive-panels.tsx      # Trash + Shared with me panels
│   │       │   ├── node-table.tsx        # read-only dense node listing + NodeIcon
│   │       │   ├── space-switcher.tsx    # rail band 1: active space + space menu
│   │       │   ├── rail-search.tsx       # rail name filter (⌘K), writes ui-store.filterQuery
│   │       │   ├── rail-item.tsx         # shared rail row geometry + section label
│   │       │   ├── storage-meter.tsx     # rail band 3: used/quota for the active space
│   │       │   ├── drive-header.tsx      # main column row 1: breadcrumbs + global toggles
│   │       │   ├── drive-toolbar.tsx     # main column row 2: create actions, sort, view
│   │       │   ├── node-list.tsx         # dense list view with checkbox multi-select
│   │       │   ├── node-grid.tsx         # grid view, grouped Folders / Files
│   │       │   ├── node-actions.tsx      # single source for a node's menu actions
│   │       │   ├── selection-bar.tsx     # floating bar for a multi-row selection
│   │       │   └── ...                  # folder-tree, preview, nav, etc. (edit is a route, not a panel)
│   │       └── routes/             # TanStack Router file-based routes
│   │           ├── profile.tsx          # user profile page
│   │           ├── admin.users.tsx      # admin: user management
│   │           ├── edit/$nodeId.tsx     # full-page OnlyOffice editor (opened in a new tab)
│   │           └── drive/$spaceId.tsx   # space drive view (leaf route — see Drive view note)
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

- **Drive view is a single leaf route.** `drive/$spaceId.tsx` owns the shell, the
  toolbar and the listing, and renders no `<Outlet/>`. Secondary surfaces are
  selected by the `panel` search param rather than by child routes:
  `?panel=members`, `?panel=trash`, `?panel=shared`; absent means the file
  listing. Adding a child route under `drive/$spaceId/` will silently never
  render — add a `panel` value, or introduce an `<Outlet/>` first.
- **Drive shell layout.** `routes/drive.tsx` owns the 256px left rail as three
  bands — `space-switcher` (48px), the scrolling nav (search, Quick access,
  My Drive tree, Spaces), and a pinned footer (`storage-meter` + `nav-user`).
  `drive/$spaceId.tsx` owns the main column as two 40px rows (`drive-header`,
  `drive-toolbar`) above the listing. Row geometry for the rail lives in
  `rail-item.tsx`; use `railRow()` rather than re-deriving row classes.
- **Node menu actions have one definition.** `node-actions.tsx` builds the
  action list; the list view, grid view and their right-click menus all render
  from it. Add an action there, not in a menu.
- **Search state is lifted.** The filter field sits in the rail (mounted by
  `drive.tsx`) but filters the listing (owned by `drive/$spaceId.tsx`), so the
  query lives in `ui-store.filterQuery` rather than in either component.
- **Design source.** The UI follows the Stitch project "pp-hub — Self-hosted
  Drive" (`11969748414038004354`); tokens and screen prompts are mirrored in
  `.stitch/DESIGN.md` and `.stitch/SCREEN-PROMPTS.md`. Where a designed element
  has no API behind it (Recent, Starred, activity feed, move/copy), it is left
  out rather than stubbed — see the comments in the component that drops it.
- The Go module lives inside the JS monorepo but is independent of bun workspaces.
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen is deferred until the API surface justifies it.
