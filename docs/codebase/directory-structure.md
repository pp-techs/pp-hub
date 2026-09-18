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
│   │       │   ├── share-dialog.tsx      # the one access surface: groups, people, link (better-modal)
│   │       │   ├── people-table.tsx      # space membership: admin switch, groups, pending invites
│   │       │   ├── group-drawer.tsx      # People page drawer: groups and who is in them
│   │       │   ├── invite-modal.tsx      # the one way into a space (better-modal)
│   │       │   ├── grantee-picker.tsx    # picks a group or a space member, filtered locally
│   │       │   ├── member-picker.tsx     # tenant user search, invite-only (see /users/search scoping)
│   │       │   ├── user-avatar.tsx       # avatar with fallback
│   │       │   ├── confirm-dialog.tsx    # confirmation modal
│   │       │   ├── quick-access.tsx      # rail links to the space's own surfaces
│   │       │   ├── trash-panel.tsx       # Trash listing + restore / delete forever / empty
│   │       │   ├── move-dialog.tsx       # "Move to…" folder picker (better-modal)
│   │       │   ├── drop-zone.tsx         # drag-and-drop overlay over the listing
│   │       │   ├── transfer-tray.tsx     # upload queue tray, mounted by the shell
│   │       │   ├── command-palette.tsx   # ⌘K search across every space
│   │       │   ├── continue-cards.tsx    # Home: the few files you were last in
│   │       │   ├── settings-tabs.tsx     # link-based tab strip for every settings page
│   │       │   ├── space-node-table.tsx  # flat listings within a space (Recent, Starred)
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
│   │           ├── login.tsx            # OIDC hand-off; ?redirect= survives the round trip
│   │           ├── admin.users.tsx      # server administration: stats, suspend, admin flag
│   │           ├── invite/$token.tsx    # redeem a space invitation
│   │           ├── s/$token.tsx         # anonymous share-link browser (no rail, no account)
│   │           ├── edit/$nodeId.tsx     # full-page OnlyOffice editor (opened in a new tab)
│   │           ├── drive.tsx            # drive shell: left rail + <Outlet/>
│   │           ├── drive/account/       # route.tsx (tabs) + profile.tsx + preferences.tsx
│   │           └── drive/$spaceId/      # route.tsx (layout) + index.tsx (listing)
│   │               │                    #   + home / recent / starred / shared / trash
│   │               └── settings/        # route.tsx (tabs) + general / people / danger
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

- **The space is a URL segment, never state.** Every drive surface is a child
  route of `drive/$spaceId/`: `index.tsx` is the listing, and `home`, `recent`,
  `starred`, `shared`, `trash` and `settings/*` sit beside it. So `spaceId` is
  in the params on every one of them, and the rail can read it without a
  fallback. This replaced a `?panel=` search param, which is why Home, Recent
  and Starred used to blank the switcher, the Files tree and the Trash and
  Shared rows: they were siblings of `$spaceId`, so there was no space to read.
  Add a surface as a child route; do not reintroduce a panel param.
- **Settings tabs are routes.** Space settings (`settings/general|people|danger`)
  and account settings (`account/profile|preferences`) both render
  `settings-tabs.tsx`, a strip of `<Link>`s. A settings pane is a place: it has
  to survive a reload, be sendable to a colleague and answer the back button.
  `settings/route.tsx` refuses a non-admin in `beforeLoad` rather than
  rendering a "you cannot" pane at a URL that resolved.
- **Drive shell layout.** `routes/drive.tsx` owns the 256px left rail as three
  bands — `space-switcher` (48px, also the only place to change space — the
  space list is not duplicated further down), the scrolling nav (search,
  Quick access, the Files tree), and a pinned footer (`storage-meter` +
  `nav-user`).
  `drive/$spaceId/route.tsx` owns `drive-header` (40px, breadcrumbs derived
  from the pathname) above an `<Outlet/>`; `index.tsx` adds `drive-toolbar`.
  The rail reads its space through `useActiveSpaceId()`, which is the route
  param everywhere except `/drive/account` — an account belongs to no space, so
  there the rail keeps pointing at the last one opened. Row geometry for the
  rail lives in `rail-item.tsx`; use `railRow()` rather than re-deriving row
  classes.
- **Folder navigation keeps the previous listing on screen.** `childrenOptions`
  sets `placeholderData: (prev) => prev` — the same pattern `useNodeSearch`
  uses — so clicking a folder in `folder-tree.tsx` swaps content in place
  instead of blanking to "Loading…" and back. `FolderTreeItem` also queries its
  **own** id (`useTree(spaceId, node.id, expanded)`), gated on `expanded`; it
  used to query `useTree(spaceId, undefined)` — the root tree's key — for
  every collapsed row, an unnecessary live subscription per rendered folder.
- **Route data loaders prefetch queries.** Routes (`drive.tsx`,
  `drive/$spaceId/index.tsx`, `s/$token.tsx`, `edit/$nodeId.tsx`,
  `admin.users.tsx`) prefetch data in route `loader`s via
  `queryClient.ensureQueryData` with plain options factories
  (`childrenOptions`, `shareInfoOptions`, `nodeOptions`, etc.), eliminating
  component mount waterfalls while preserving component-level subscriptions.
- **The archive is filtered out of the ordinary listing.** `spaces.archive_root_id`
  is a real entry point (the `Admin` group holds a grant on it, see
  [permissions.md](../architecture/permissions.md#archive)), so the server
  returns it like any other root-level folder. Both `index.tsx` (main listing)
  and `folder-tree.tsx` (rail) filter it out by id when browsing the space
  root; `quick-access.tsx` renders it as its own row instead, visible only to
  `space.is_admin`. Browsing into it (`?dir=archive_root_id`) is ordinary —
  filtering only applies at the root, where it would otherwise sit mixed in
  with real files.
- **Clicking a node only selects it.** There is no checkbox: a plain click
  replaces the selection, ⌘/Ctrl toggles one node and Shift takes the range
  from the last plain click, all of it decided by `nextSelection`
  (`lib/selection.ts`) so the grid and the list cannot drift. Selected nodes
  get a soft `bg-primary/10` wash, never an outline.
- **The listing's background is a marquee.** `useMarqueeSelection`
  (`lib/use-marquee-selection.ts`) is mounted on the scrolling listing in
  `drive/$spaceId/index.tsx`: a press bands every node box it overlaps, holding
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
- **People is one table, and the only membership surface.** `people-table.tsx`
  owns members, pending invitations, the Admin switch and the group chips;
  `group-drawer.tsx` owns the group list beside it, and `invite-modal.tsx` is
  the only way into a space. Admin _is_ membership of the automatic `Admin`
  group, so the switch and the chip are one fact — which is why all three
  membership mutations invalidate `["spaces", id]` rather than just the member
  list, and why that group is read-only in the drawer. What a group _opens_ is
  still set on the folder, in its access dialog: this table answers who is in
  the group, the folder answers what the group reaches. Listing invitations is
  admin-gated server-side, so the query is `enabled` on the admin check rather
  than firing and 403-ing.
- **Node menu actions have one definition.** `node-actions.tsx` builds the
  action list; the list view, grid view and their right-click menus all render
  from it. Add an action there, not in a menu.
- **Two searches, on purpose.** The rail field filters the _open folder_
  locally — its query lives in `ui-store.filterQuery` because the field sits in
  the rail (mounted by `drive.tsx`) while the listing it filters is owned by
  `drive/$spaceId/index.tsx`. ⌘K is something else: a server search across every
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
  automatic `Admin` group — administers one space
  (`/drive/$spaceId/settings/*`, reached from the space menu in the rail); a
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
