# Directory Structure

Monorepo managed with bun workspaces + Vite+ (`vp`). Workspaces: `packages/*`, `apps/*`, `tools/*` (declared in root `package.json`; `tools/` is reserved and currently empty — deploy lives at `deploy/`).

```
pp-hub/
├── apps/
│   ├── web/                        # React SPA
│   │   ├── Dockerfile              # Vite build → nginx; the public entry point in prod
│   │   ├── nginx.conf              # serves the bundle, proxies the API on one origin
│   │   └── src/
│   │       ├── lib/
│   │       │   ├── api/            # ky client, types, endpoints, utils
│   │       │   ├── queries/        # TanStack Query hooks (per-resource)
│   │       │   ├── query-client.ts # shared QueryClient
│   │       │   ├── ui-store.ts     # zustand ephemeral UI state
│   │       │   ├── selection.ts    # click → selection (replace / ⌘ toggle / ⇧ range)
│   │       │   ├── use-marquee-selection.ts # drag a band over the listing to select
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
│   │       │   ├── node-table.tsx        # read-only dense node listing
│   │       │   ├── node-icon.tsx         # NodeIcon: vscode-icons glyph per extension
│   │       │   ├── space-switcher.tsx    # rail band 1: active space + space menu
│   │       │   ├── rail-search.tsx       # rail name filter (⌘K), writes ui-store.filterQuery
│   │       │   ├── rail-item.tsx         # shared rail row geometry + section label
│   │       │   ├── storage-meter.tsx     # rail band 3: used/quota for the active space
│   │       │   ├── drive-header.tsx      # main column row 1: breadcrumbs + global toggles
│   │       │   ├── drive-toolbar.tsx     # main column row 2: create, sort, view, details (i)
│   │       │   ├── node-list.tsx         # dense list view, click-to-select rows
│   │       │   ├── node-grid.tsx         # card grid, grouped Folders / Files, always-on thumbnails
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
│       ├── cmd/server/             # entry point: OIDC wait, http.Server; owns migrations
│       ├── cmd/worker/             # optional preview worker (pdftoppm/ffmpeg); --backfill
│       ├── Dockerfile              # targets: worker (poppler+ffmpeg), server (slim, default)
│       ├── .env.example            # → .env (git-ignored), sourced for local dev
│       └── internal/
│           ├── bootstrap/          # ✅ shared wiring for both entry points: config, pool, blobs
│           ├── activity/           # ✅ event recording: who did what, to what
│           ├── auth/               # ✅ OIDC login/callback/logout, HMAC session cookies (go-oidc)
│           ├── gc/                 # ✅ blob reclaim + trash retention sweeps
│           ├── config/             # ✅ env-based config
│           ├── httpapi/            # ✅ chi routes (/healthz, /auth/*, /api/v1/*), RFC 7807 errors
│           │   ├── users.go        # user profile CRUD + search endpoints
│           │   ├── groups.go       # groups API + the folder-grant endpoints
│           │   └── spaces_members.go # space members API (list/add/remove, set-admin)
│           ├── store/              # ✅ sqlc queries, embedded migrations runner
│           │   └── migrations/     # the whole schema as one baseline: 0001_init.sql
│           ├── users/              # user service layer (profile, search)
│           ├── spaces/             # space CRUD, membership, the Admin group, leaving a space
│           │   ├── spaces.go       # RequireAdmin/SetAdmin/AddMember; RemoveMember archives what a leaver owned
│           │   └── permissions_db_test.go # DB-backed; skipped unless PP_TEST_DATABASE_URL is set
│           ├── groups/             # groups + the grants that open folders
│           ├── fs/                 # file tree, versioning, move/copy/rename
│           ├── blob/               # CAS Store interface + local impl (S3 impl later)
│           ├── share/              # grants — user, group, link — and the caps they resolve to
│           ├── wopi/               # WOPI host endpoints, access tokens
│           └── preview/            # renderers + job loop; jobs.kind routes, previews.kind is the artifact
├── packages/
│   ├── ui/                         # @workspace/ui — shadcn/ui preset components + design tokens (globals.css)
│   └── utils/                      # shared TS utils (template package, dist via vp pack)
├── deploy/
│   └── compose/
│       ├── compose.dev.yml         # services only — app runs from source; profiles: worker, s3, idp
│       ├── compose.prod.yml        # everything, for a VPS / Dokploy; profile: s3
│       ├── .env.dev.example        # → .env (git-ignored), dev values
│       ├── .env.prod.example       # → .env (git-ignored), production values
│       └── seaweedfs/s3.json       # dev credentials for the S3 profile
├── docs/                           # this documentation
└── package.json                    # bun workspaces, Vite+ catalog
```

## Entry Points

- Web dev: `vp run web#dev` (root `vp run dev`); Vite proxies `/api`, `/auth`, `/s` → `localhost:8080`.
- Server local: `cp apps/server/.env.example apps/server/.env` (fill `PP_OIDC_*`), then `set -a && source .env && set +a && go run ./cmd/server` from `apps/server`. The binary has no dotenv loader, so the file must be sourced.
- Dev stack: `docker compose -f deploy/compose/compose.dev.yml up -d` — postgres + OnlyOffice at :9980. Profiles add the worker, SeaweedFS and a self-hosted Logto. Full setup: [docs/deployment.md](../deployment.md).

## Notes

- **Drive view is a single leaf route.** `drive/$spaceId.tsx` owns the shell, the
  toolbar and the listing, and renders no `<Outlet/>`. Secondary surfaces are
  selected by the `panel` search param rather than by child routes:
  `?panel=members`, `?panel=trash`, `?panel=shared`, `?panel=settings`; absent means the file
  listing. Adding a child route under `drive/$spaceId/` will silently never
  render — add a `panel` value, or introduce an `<Outlet/>` first.
- **Drive shell layout.** `routes/drive.tsx` owns the 256px left rail as three
  bands — `space-switcher` (48px), the scrolling nav (search, Quick access,
  the Files tree, Spaces), and a pinned footer (`storage-meter` + `nav-user`).
  `drive/$spaceId.tsx` owns the main column as two 40px rows (`drive-header`,
  `drive-toolbar`) above the listing. Row geometry for the rail lives in
  `rail-item.tsx`; use `railRow()` rather than re-deriving row classes.
- **Clicking a node only selects it.** There is no checkbox: a plain click
  replaces the selection, ⌘/Ctrl toggles one node and Shift takes the range
  from the last plain click, all of it decided by `nextSelection`
  (`lib/selection.ts`) so the grid and the list cannot drift. Selected nodes
  get a soft `bg-primary/10` wash, never an outline.
- **The listing's background is a marquee.** `useMarqueeSelection`
  (`lib/use-marquee-selection.ts`) is mounted on the scrolling listing in
  `drive/$spaceId.tsx`: a press bands every node box it overlaps, holding
  ⌘/Ctrl/Shift adds to whatever was selected when the drag began, and a press
  that never travels 4px reports an empty band — which is what clears the
  selection on a plain background click. A press is only background if it is
  outside `[data-node-id]` (a card or row) and `[data-keeps-selection]` (the
  selection bar); anything new that floats over the listing and acts on the
  selection needs that second attribute, or pressing it will clear first. The
  band is held in the container's content coordinates, so a mid-drag scroll
  leaves it over the nodes it was drawn across.
- **The details panel never opens by itself.** It is the toolbar's (i) toggle
  next to the view switch, or a node's "Information" action, which selects that
  node alone and opens the panel. It then follows the selection: one node shows
  the full sheet, several show "x folders and y files selected"
  (`describeSelection`), none shows a prompt. Selection also feeds the bulk
  `selection-bar` — one piece of state for both, so clicking a node cannot
  silently fetch shares and versions for it.
- **File glyphs come from `vscode-icons`, keyed by extension.** `node-icon.tsx`
  maps an extension to one `~icons/vscode-icons/*` component; `unplugin-icons`
  (wired in `apps/web/vite.config.ts`, typed by the `unplugin-icons/types/react`
  entry in `tsconfig.json`) inlines the SVG at build time, so nothing calls the
  Iconify API at runtime. The glyphs carry their own colours — size them, never
  tint them. New file type: add one import plus one `BY_EXT` row.
- **Grid cards always show the thumbnail frame.** A file card is a header row
  (glyph, name, ⋯) above a fixed 144px frame holding
  `/preview?kind=thumbnail`. A file the preview worker never rendered one for —
  anything but a raster image, see
  [preview-pipeline.md](../architecture/preview-pipeline.md) — shows the frame
  empty rather than an enlarged icon, as does a thumbnail that fails to load.
  Size and date live in the card's tooltip; the list view and details panel are
  where numbers belong. Column count comes from a container query on the grid
  itself (2→5 columns), so opening the details panel drops 5 columns to 4
  without the route passing anything down.
- **Members and invitations render as one table.** `space-members-panel.tsx`
  owns both, plus the admin toggle and the invite dialog; `space-settings-panel.tsx`
  only mounts it. Listing invitations is admin-gated server-side, so the query
  is `enabled` on the admin check rather than firing and 403-ing.
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
- **Two kinds of administration.** A _space admin_ — a member of that space's
  automatic `Admin` group — administers one space (`?panel=settings`); a
  _server admin_ administers the install (`/admin/users`). Neither implies the
  other, and neither carries file access: an admin reads a colleague's file
  only through a grant. The server enforces that separately from what the UI
  renders.
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
