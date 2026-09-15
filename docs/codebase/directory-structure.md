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
│   │       │   ├── ui-store.ts     # zustand ephemeral UI state
│   │       │   ├── upload-store.ts # zustand upload queue (concurrency, retry, mkdir cache)
│   │       │   ├── drop-items.ts   # flattens a drop / folder pick into upload items
│   │       │   ├── use-debounced.ts # trailing-edge debounce for the palette query
│   │       │   └── use-keyed-state.ts # state that resets when its subject changes
│   │       ├── components/         # app-level UI panels
│   │       │   ├── share-panel.tsx       # share dialog (direct + link)
│   │       │   ├── space-members-panel.tsx # members + pending invites in one table
│   │       │   ├── space-groups-panel.tsx  # Groups tab: groups, their people, what they open
│   │       │   ├── folder-access-dialog.tsx # who can open a folder (better-modal, admin only)
│   │       │   ├── member-picker.tsx     # user search + member add
│   │       │   ├── user-avatar.tsx       # avatar with fallback
│   │       │   ├── confirm-dialog.tsx    # confirmation modal
│   │       │   ├── quick-access.tsx      # rail entries for the Trash / Shared panels
│   │       │   ├── drive-panels.tsx      # Shared with me panel
│   │       │   ├── trash-panel.tsx       # Trash listing + restore / delete forever / empty
│   │       │   ├── move-dialog.tsx       # "Move to…" folder picker (better-modal)
│   │       │   ├── drop-zone.tsx         # drag-and-drop overlay over the listing
│   │       │   ├── transfer-tray.tsx     # upload queue tray, mounted by the shell
│   │       │   ├── command-palette.tsx   # ⌘K search across every space
│   │       │   ├── continue-cards.tsx    # Home: the few files you were last in
│   │       │   ├── space-settings-panel.tsx # General / Members / Groups / Danger zone tabs
│   │       │   ├── cross-space-table.tsx # listings spanning spaces (Recent, Starred)
│   │       │   ├── activity-feed.tsx     # the event timeline
│   │       │   ├── storage-breakdown.tsx # usage by MIME family
│   │       │   ├── version-list.tsx      # shared by the ⋯ dialog and preview tab
│   │       │   ├── node-table.tsx        # read-only dense node listing + NodeIcon
│   │       │   ├── space-switcher.tsx    # rail band 1: active space + space menu
│   │       │   ├── rail-search.tsx       # rail name filter (⌘K), writes ui-store.filterQuery
│   │       │   ├── rail-item.tsx         # shared rail row geometry + section label
│   │       │   ├── storage-meter.tsx     # rail band 3: used/quota for the active space
│   │       │   ├── drive-header.tsx      # main column row 1: breadcrumbs + global toggles
│   │       │   ├── drive-toolbar.tsx     # main column row 2: create actions, sort, view
│   │       │   ├── node-list.tsx         # dense list view with checkbox multi-select
│   │       │   ├── node-grid.tsx         # grid view, grouped Folders / Files, multi-select
│   │       │   ├── node-actions.tsx      # single source for a node's menu actions
│   │       │   ├── selection-bar.tsx     # floating bar for a multi-row selection
│   │       │   └── ...                  # folder-tree, preview, nav, etc. (edit is a route, not a panel)
│   │       └── routes/             # TanStack Router file-based routes
│   │           ├── profile.tsx          # user profile page
│   │           ├── admin.users.tsx      # server administration: stats, suspend, admin flag
│   │           ├── invite/$token.tsx    # redeem a space invitation
│   │           ├── s/$token.tsx         # anonymous share-link browser (no rail, no account)
│   │           ├── drive/home.tsx       # cross-space dashboard
│   │           ├── drive/recent.tsx     # files I last touched
│   │           ├── drive/starred.tsx    # my bookmarks
│   │           ├── edit/$nodeId.tsx     # full-page OnlyOffice editor (opened in a new tab)
│   │           └── drive/$spaceId.tsx   # space drive view (leaf route — see Drive view note)
│   └── server/                     # Go backend — own go.mod
│       ├── cmd/server/             # entry point: config, PG pool, migrate, OIDC wait, http.Server
│       └── internal/
│           ├── activity/           # ✅ event recording: who did what, to what
│           ├── auth/               # ✅ OIDC login/callback/logout, HMAC session cookies (go-oidc)
│           ├── gc/                 # ✅ blob reclaim + trash retention sweeps
│           ├── config/             # ✅ env-based config
│           ├── httpapi/            # ✅ chi routes (/healthz, /auth/*, /api/v1/*), RFC 7807 errors
│           │   ├── users.go        # user profile CRUD + search endpoints
│           │   └── spaces_members.go # space members API (list/add/update/remove)
│           ├── store/              # ✅ sqlc queries, embedded migrations runner
│           ├── users/              # user service layer (profile, search)
│           ├── spaces/             # space CRUD, member roles, personal folders
│           ├── groups/             # groups + the grants that open folders
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
  `?panel=members`, `?panel=trash`, `?panel=shared`, `?panel=settings`; absent means the file
  listing. Adding a child route under `drive/$spaceId/` will silently never
  render — add a `panel` value, or introduce an `<Outlet/>` first.
- **Drive shell layout.** `routes/drive.tsx` owns the 256px left rail as three
  bands — `space-switcher` (48px), the scrolling nav (search, Quick access,
  My Drive tree, Spaces), and a pinned footer (`storage-meter` + `nav-user`).
  `drive/$spaceId.tsx` owns the main column as two 40px rows (`drive-header`,
  `drive-toolbar`) above the listing. Row geometry for the rail lives in
  `rail-item.tsx`; use `railRow()` rather than re-deriving row classes.
- **Selection and focus are two different things.** A ticked checkbox feeds the
  bulk `selection-bar`; the last single-clicked row feeds the details panel.
  `node-list.tsx` and `node-grid.tsx` both take `selectedIds` _and_ `focusedId`
  and style them apart — collapsing them back into one flag is what made the
  grid unable to multi-select.
- **Members and invitations render as one table.** `space-members-panel.tsx`
  owns both, plus the role reference and the invite dialog; `space-settings-panel.tsx`
  only mounts it. Listing invitations is owner-gated server-side, so the query
  is `enabled` on the owner check rather than firing and 403-ing.
- **Node menu actions have one definition.** `node-actions.tsx` builds the
  action list; the list view, grid view and their right-click menus all render
  from it. Add an action there, not in a menu.
- **Two searches, on purpose.** The rail field filters the _open folder_
  locally — its query lives in `ui-store.filterQuery` because the field sits in
  the rail (mounted by `drive.tsx`) while the listing it filters is owned by
  `drive/$spaceId.tsx`. ⌘K is something else: a server search across every
  space, in `command-palette.tsx`. The rail's `⌘K` chip is a button through to
  the palette, not a hint that the chip focuses the field. Don't merge them.
- **Design source.** The UI follows the Stitch project "pp-hub — Self-hosted
  Drive" (`11969748414038004354`); tokens and screen prompts are mirrored in
  `.stitch/DESIGN.md` and `.stitch/SCREEN-PROMPTS.md`. Where a designed element
  has no API behind it (comments, collaborator presence, per-space sharing
  policy, integrations), it is left out rather than stubbed — see the comments
  in the component that drops it.
- **Web tests that need a DOM** declare it per file with
  `// @vitest-environment happy-dom`; everything else stays in node, which is
  faster. Testing Library's auto-cleanup only fires when the test framework
  exposes globals, and this suite imports explicitly, so component test files
  register `afterEach(cleanup)` themselves. `apps/web/tsconfig.json` includes
  `tests` so JSX in a test type-checks.
- **Two kinds of administration.** A _space owner_ administers one space
  (`?panel=settings`); a _server admin_ administers the install
  (`/admin/users`). Neither role implies the other, and the server enforces
  that separately from what the UI renders.
- **better-modal keeps a closed modal MOUNTED.** `modal.resolve()` only flips
  `isOpen` to false; the entry stays in the store and `ModalContainer` goes on
  rendering the component. So `useState` inside a modal initialises **once for
  the life of the app**, and reopening for a different item leaves the previous
  item's state in place. Any modal holding state about its subject must key
  that state to the subject — use `useKeyedState` rather than a fresh
  `useState`. This caused the preview modal to reopen on the last file viewed,
  and the move dialog to keep a destination the user had not selected.
- **Uploads do not live in TanStack Query.** A mutation models one request with
  one result; the transfer tray needs a queue with per-file progress, retry and
  cancellation, and it has to outlive the route that started it. So uploads run
  through `lib/upload-store.ts` and `TransferTray` is mounted by `drive.tsx`,
  not by the folder view. The store cannot reach the query client, so it
  publishes changes through `onUploadSettled` and the tray invalidates. It
  fires for a created folder as well as a finished file — otherwise uploading a
  folder only ever names directories below the view you are looking at, and the
  new folder wouldn't appear in it until a reload.
  `uploadFileWithProgress` is the one endpoint that uses `XMLHttpRequest`
  rather than the shared `ky` client, because `fetch` reports no upload
  progress.
- The Go module lives inside the JS monorepo but is independent of bun workspaces.
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen is deferred until the API surface justifies it.
