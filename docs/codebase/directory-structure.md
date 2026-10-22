# Directory Structure

Monorepo managed with bun workspaces + Vite+ (`vp`). Workspaces: `packages/*`, `apps/*`, `tools/*` (declared in root `package.json`; `tools/release` is the version carrier for the release flow — deploy lives at `deploy/`).

```
pp-hub/
├── apps/
│   ├── web/                        # React SPA
│   │   ├── scripts/mdoc-fixtures.ts # writes the Go mdoc golden fixtures from slash-editor itself
│   │   ├── tests/mdoc-go-updates.test.ts # slash-editor must read the Go-built room seeds back
│   │   └── src/
│   │       ├── lib/
│   │       │   ├── api/            # ky client, types, endpoints, utils; tus.ts = resumable upload client
│   │       │   ├── collab/         # block documents: use-collab-room.ts (room, epochs, recheck, pagehide),
│   │       │   │                   #   mdoc-kit.ts (the block set, shared with the renderer and the fixture script),
│   │       │   │                   #   attachments.ts (tus upload with attach_to, altFromFileName),
│   │       │   │                   #   presence.ts (colour + initials per user), mentions.ts (@ people),
│   │       │   │                   #   page-store.ts (M43: the host PageStore over the query cache, room `meta`)
│   │       │   ├── comments.ts     # comment thread store over REST: 10 s poll while a document is open (M29)
│   │       │   ├── event-stream.ts # SSE subscription with capped exponential reconnect (notifications)
│   │       │   ├── queries/        # TanStack Query hooks (per-resource); pages.ts = page/lineage/children/backlinks
│   │       │   │                   #   options under ['pages', scope, …] (M43); publications.ts = a node's publication (M50)
│   │       │   ├── query-client.ts # shared QueryClient
│   │       │   ├── ext-name.ts, ext-notification.ts # M44: an extension's translated name; how the bell words ext:<id>:<verb> notifications
│   │       │   ├── app-package.ts  # M51: zips a picked folder in the browser (fflate) for a packaged-app upload
│   │       │   ├── ui-store.ts     # zustand ephemeral UI state
│   │       │   ├── safe-path.ts    # the SPA half of auth.safeNext: a same-origin path, or nothing
│   │       │   ├── user-agent.ts   # "Chrome on macOS" from a UA string, for the session and log lists
│   │       │   ├── selection.ts    # click → selection (replace / ⌘ toggle / ⇧ range)
│   │       │   ├── use-marquee-selection.ts # drag a band over the listing to select
│   │       │   ├── drag-drop.ts    # validation, cycle detection & ghost badge for internal DnD
│   │       │   ├── upload-store.ts # zustand upload queue (concurrency, pause/resume, retry, mkdir cache) over tus
│   │       │   ├── drop-items.ts   # flattens a drop / folder pick into upload items
│   │       │   ├── use-debounced.ts # trailing-edge debounce for the palette query
│   │       │   └── use-keyed-state.ts # state that resets when its subject changes
│   │       ├── ext/                # host side of the apps: types.ts, registry.ts (installed packaged apps from /api/v1/extensions, registerPackagedApps), ext-unavailable.tsx;
│   │       │                       #   packaged.tsx (ExtManifest from the listing, pp-app:<id>:<English> strings, icons),
│   │       │                       #   app-frame.tsx (the sandboxed iframe), app-host.ts (attachFrame, the bridge method table), app-bridge.ts (serveBridge, param checks)
│   │       ├── components/         # app-level UI panels
│   │       │   ├── share-dialog.tsx      # the one access surface: groups, people, link (better-modal)
│   │       │   ├── publish-dialog.tsx    # M50: publish / republish / unpublish a page or folder, entry, password, expiry (better-modal)
│   │       │   ├── apps/                 # M51 Admin › Apps dialogs (better-modal): upload-app-modal.tsx, app-backend-modal.tsx, remove-app-modal.tsx
│   │       │   ├── people-table.tsx      # space membership: fixed-height rows, per-row ⋯ menu (admin, groups, remove), pending handover
│   │       │   ├── group-list.tsx        # Groups pane: the space's groups and who is in them
│   │       │   ├── invite-modal.tsx      # the one way into a space: an email + the admin toggle (better-modal)
│   │       │   ├── no-admin-banner.tsx   # "this space has no administrator", above the space Outlet
│   │       │   ├── unlinked-space-alert.tsx # People pane: the space mirrors no Logto organization; instance admins get Link to Logto
│   │       │   ├── link-space-modal.tsx  # Link to Logto: pushable vs stranded members, stranded need a confirm (better-modal)
│   │       │   ├── grantee-picker.tsx    # picks a group or a space member, filtered locally
│   │       │   ├── user-avatar.tsx       # avatar with fallback
│   │       │   ├── space-avatar.tsx      # the space's picture, editable by its admins
│   │       │   ├── confirm-dialog.tsx    # confirmation modal
│   │       │   ├── app-sidebar.tsx       # the rail: header + nav bands + footer, one Sidebar
│   │       │   ├── nav-create.tsx        # rail: the accented New row (upload files/folder, mkdir)
│   │       │   ├── nav-space.tsx         # rail: Home / My Drive+tree / smart views / Trash+Archive
│   │       │   ├── trash-panel.tsx       # Trash listing + restore / delete forever / empty
│   │       │   ├── move-dialog.tsx       # "Move to…" folder picker (better-modal)
│   │       │   ├── drop-zone.tsx         # drag-and-drop overlay over the listing
│   │       │   ├── transfer-tray.tsx     # upload queue tray, mounted by the shell
│   │       │   ├── continue-cards.tsx    # Home: the few files you were last in
│   │       │   ├── command-palette.tsx   # ⌘K search across every space; content matches show a highlighted snippet
│   │       │   ├── doc-editor/           # the block editor: doc-editor.tsx, outline-panel.tsx, lossy-edit-modal.tsx, labels.ts;
│   │       │   │                         #   M43 page-header.tsx (title, icon, cover), page-tree-panel.tsx, page-backlinks.tsx
│   │       │   ├── nodes/                # slash-editor node views we own: image, file, video, embed (+ embed-link-modal),
│   │       │   │                         #   mention, mermaid (attachment: src); page-node-views.tsx (subPage, pageLink — M43)
│   │       │   ├── emoji-menu.tsx        # the `:` emoji picker (M43)
│   │       │   ├── page-trail.tsx        # a page's breadcrumb through its parent documents (M43)
│   │       │   ├── sub-page-count.tsx    # listing badge: how many sub-pages a document holds (M43)
│   │       │   ├── markdown-document-view/ # read-only renderer of GET …/document: preview, version preview, share page
│   │       │   ├── table-controls.tsx    # block editor: add/delete rows and columns, header toggle, delete table
│   │       │   ├── new-document-dialog.tsx # New → Document/Spreadsheet/…; the markdown template picker
│   │       │   ├── version-preview-modal.tsx # a rendered markdown version with Restore
│   │       │   ├── comments/             # block comment buttons, comments popover, composer (with @mention), avatars (M29)
│   │       │   ├── notifications-bell.tsx # unread badge + list in the site and editor headers (M29)
│   │       │   ├── settings/             # the one settings dialog (see below)
│   │       │   │   ├── settings-modal.tsx       # shell: nav, header, unsaved-changes guard
│   │       │   │   ├── settings-hash-bridge.tsx # `#settings/<pane>` → open/closed modal
│   │       │   │   ├── panes.ts                 # registry: slug, group, label, icon, gate
│   │       │   │   ├── settings-dirty.tsx       # a pane reports unsaved edits to the shell
│   │       │   │   └── panes/                   # account-{general,profile,security}
│   │       │   │                                #   space-{general,members,groups,danger}
│   │       │   │                                #   account-security's sections: agent-access-section (MCP),
│   │       │   │                                #   webdav-section (M47 mount), personal-tokens (shared token list, per purpose)
│   │       │   ├── space-node-table.tsx  # flat listings within a space (Recent, Starred)
│   │       │   ├── activity-feed.tsx     # the event timeline
│   │       │   ├── storage-breakdown.tsx # usage by MIME family
│   │       │   ├── version-list.tsx      # shared by the ⋯ dialog and preview tab
│   │       │   ├── node-table.tsx        # read-only dense node listing
│   │       │   ├── node-icon.tsx         # NodeIcon: vscode-icons glyph per extension
│   │       │   ├── space-switcher.tsx    # SidebarHeader: active space + the only space menu
│   │       │   ├── nav-search.tsx        # header ⌘K trigger; icon-only below `sm`
│   │       │   ├── section-label.tsx     # uppercase 11px heading for a page or form section
│   │       │   ├── storage-meter.tsx     # SidebarFooter: used/quota for the active space
│   │       │   ├── site-header.tsx       # the one header: trigger + breadcrumb + HeaderSlot
│   │       │   ├── header-slot.tsx       # portal pair so a route fills the shell's header
│   │       │   ├── listing-controls.tsx  # sort, view and details (i), portalled by the listing
│   │       │   ├── surface.tsx           # scrolling body + page rhythm for every non-listing surface
│   │       │   ├── node-list.tsx         # dense list view, click-to-select rows
│   │       │   ├── node-grid.tsx         # card grid, grouped Folders / Files, always-on thumbnails
│   │       │   ├── node-actions.tsx      # single source for a node's menu actions
│   │       │   ├── selection-bar.tsx     # floating bar for a multi-row selection
│   │       │   ├── loading.tsx           # shared skeletons + FetchingBar; no bare "Loading…" lines
│   │       │   ├── admin-sidebar.tsx     # the /admin rail: nav (ADMIN_NAV), back to Drive, signed-in user
│   │       │   ├── admin-page.tsx        # AdminPage: title + description + actions frame for every admin page
│   │       │   ├── admin-settings-ui.tsx # SettingsSection / SettingRow / UnitInput / SourceBadge for instance settings
│   │       │   ├── file-view.tsx         # one open file: block editor / OnlyOffice / app editor / viewer, by type
│   │       │   ├── editor-header.tsx     # an open file's breadcrumb (header lead) and controls (header actions), by portal
│   │       │   └── ...                  # folder-tree, preview, nav, etc.
│   │       └── routes/             # TanStack Router file-based routes
│   │           ├── login.tsx            # OIDC hand-off; ?redirect= survives the round trip
│   │           ├── admin.tsx            # server-administration shell: layout route, admin gate, AdminSidebar + SidebarInset
│   │           ├── admin.index.tsx      # /admin → /admin/users
│   │           ├── admin.users.tsx      # users: stat tiles, searchable table, ⋯ menu (admin flag, suspend)
│   │           ├── admin.security.tsx   # the security log: filters (kind, file, dates), CSV export
│   │           ├── admin.settings.tsx   # instance settings: tabs (`?tab=` general/storage/security/sharing/mail), grouped setting rows, sticky save bar, SMTP
│   │           ├── admin.apps.tsx       # Admin › Apps: instance switches; M51 Upload app; per app Upload new version / Backend / Remove
│   │           ├── s/$token.tsx         # anonymous share-link browser (no rail, no account)
│   │           ├── p/$token.tsx         # M50: published-page viewer — bar, password form, sandboxed iframe on /a/ or PP_ARTIFACT_URL
│   │           ├── edit/$nodeId.tsx     # legacy address (stored page links, bookmarks) → /open/$nodeId
│   │           ├── open.$nodeId.tsx     # resolves a node to its file route or folder
│   │           ├── drive.tsx            # drive shell: SidebarProvider + AppSidebar + SidebarInset (header, Outlet)
│   │           ├── drive/shared_.file.$nodeId.tsx # a file shared from outside, rendered in the shell
│   │           └── drive/$spaceId/      # route.tsx (space guard) + index.tsx (listing)
│   │                                    #   + home / recent / starred / shared / trash
│   │                                    #   + file.$nodeId.tsx (an open file, rendered by its route)
│   └── server/                     # Go backend — own go.mod
│       ├── cmd/server/             # entry point: OIDC wait, http.Server; owns migrations; binds before collab Start (listenThenStart)
│       │                           #   (apps are uploaded at runtime, not compiled in: internal/apppkg + internal/exthost)
│       ├── cmd/worker/             # optional preview worker (pdftoppm/ffmpeg); --backfill (previews + text extraction)
│       ├── Dockerfile              # build context = repo root; build-web (bun/vite) stage; targets: worker (poppler+ffmpeg), server (slim, default; ships the SPA at /srv/web); copies third_party/ and the package manifests of packages/app-sdk, examples/apps/{kanban,checklist} and tools/pp-apps
│       ├── third_party/ygo/        # reearth/ygo v1.50.0, patched (PATCHES.md); go.mod `replace`s onto it
│       ├── .env.example            # → .env (git-ignored), sourced for local dev
│       └── internal/
│           ├── bootstrap/          # ✅ shared wiring for both entry points: config, pool, blobs
│           ├── activity/           # ✅ per-space feed: who did what, to what
│           ├── audit/              # ✅ per-instance security log: sign-ins, failures, access changes, reads
│           │                       #    Distinct from activity on purpose — see data-model.md
│           │                       #    reads.go: RecordRead, deduplicated per (kind, actor, node) per hour
│           ├── alert/              # ✅ watches security_events: failed sign-in bursts → alert row, metric, admin email
│           ├── auth/               # ✅ OIDC (PKCE + nonce) + DB-backed sessions (go-oidc)
│           ├── gc/                 # ✅ blob reclaim + trash retention sweeps
│           ├── config/             # ✅ env config; DeriveKey issues purpose-bound subkeys
│           ├── httpapi/            # ✅ chi routes (/healthz, /auth/*, /api/v1/*, /wopi/*, /s/*, /p/*, /hooks/logto; web.go serves the SPA when PP_WEB_DIR is set; WopiRouter on its own :8081 listener; the artifact router by /a prefix or, with PP_ARTIFACT_URL, by Host)
│           │   ├── server.go       # the router, and the middleware that makes it safe by group
│           │   ├── csrf.go         # sameOrigin: every mutation, before authentication; socketOrigin: the collab upgrade
│           │   ├── collab.go       # GET /nodes/{id}/collab, the /collab/{room} socket, /nodes/{id}/attachments/{hash}
│           │   ├── documents.go    # block documents outside the editor: /document, /export, /unfurl, /templates/markdown,
│           │   │                   #    /s/{token}/document and /attachments/{hash}; Range for attachments
│           │   ├── pages.go        # M43: /nodes/{id}/page, /pages, /backlinks, /s/{token}/page and /pages, child_page_count
│           │   ├── comments.go     # comment threads and comments (M29)
│           │   ├── notifications.go # notification list, unread count, mark read, the SSE /stream
│           │   ├── search.go       # GET /search: name + content matches, snippets; parent_name as a path through parent pages
│           │   ├── ratelimit.go    # per-IP token buckets on the anonymous surfaces
│           │   ├── limits.go       # body ceilings outside tus: limitBody (WOPI PutFile), parseUpload (avatar)
│           │   ├── uploads.go      # tus 1.0.0 /api/v1/uploads — every browser upload; WOPI PutFile is the other writer
│           │   ├── logging.go      # chi's logger with share, publication, artifact-path and WOPI tokens redacted
│           │   ├── sessions.go     # the account's own device list and its revoke endpoints
│           │   ├── security_events.go # GET /admin/security-events (+ filters) and its CSV /export
│           │   ├── users.go        # user profile CRUD
│           │   ├── groups.go       # groups API + the folder-grant endpoints
│           │   ├── webhooks.go     # POST /hooks/logto — HMAC over the raw body, then enqueue an org or user reconcile
│           │   ├── mcp.go          # M41: /mcp/ (bearer-only, mcpBearer) + /.well-known/oauth-protected-resource
│           │   ├── agent.go        # M41: agentAPI — tool calls dispatched in process through apiRoutes (innerRequest, shared with /dav)
│           │   ├── agent_text.go   # M41: GET/PUT /nodes/{id}/text and GET /nodes/{id}/file, mounted only on the agent router
│           │   ├── publish.go      # M50: /api/v1/nodes/{id}/publication (GET/POST/PATCH/DELETE), /p/{token} and /unlock
│           │   ├── artifact.go     # M50: the artifact router — under /a on the app's origin, or by Host on PP_ARTIFACT_URL; sandbox CSP, GET/HEAD of a snapshot
│           │   ├── apps.go         # M51: /api/v1/admin/apps (install/upgrade, get, PATCH backend_url, DELETE ?purge, backend-secret); /x/ asset switch + app CSP
│           │   ├── agent_publish.go # M50: PUT /nodes/{id}/site-files, publish_artifact's write, mounted only on the agent router
│           │   ├── agent_tokens.go # M41: personal access tokens (/users/me/agent-tokens; purpose mcp|dav since M47), never on the agent router
│           │   ├── dav.go          # M47: /dav — davAuth (Basic/Bearer dav token), reads, DELETE, MKCOL
│           │   ├── dav_tree.go     # M47: namespace, listings, x/net/webdav adapters (davFS, davFile, davInfo, davLocks)
│           │   ├── dav_write.go    # M47: PUT, COPY, MOVE through the agent router; conflicted copies, safe saves
│           │   ├── dav_state.go    # M47: in-memory davState — remembered reads, tombs, scratch, pages folders, locks
│           │   ├── dav_names.go    # M47: labels and scratch-file names
│           │   └── spaces_members.go # space members API (list/add/remove, set-admin) + the departure queue
│           ├── store/              # ✅ sqlc queries, embedded migrations runner
│           │   └── migrations/     # 0001_init.sql baseline, then one file per change:
│           │                       #    0002_sessions.sql, 0003_security_events.sql,
│           │                       #    0004_user_avatars.sql, 0005_logto_orgs.sql,
│           │                       #    0006_system_config.sql, 0007_idp_profile.sql,
│           │                       #    0008_read_audit.sql, 0009_direct_share_uniq.sql,
│           │                       #    0010_resumable_uploads.sql, 0011_blob_integrity.sql,
│           │                       #    0012_detached_orgs.sql, 0013_block_documents.sql,
│           │                       #    0014_markdown_mime.sql, 0015_comments_notifications.sql,
│           │                       #    0016_content_search.sql, 0017_space_avatars.sql,
│           │                       #    0018_agent_tokens.sql, 0019_personal_spaces.sql,
│           │                       #    0020_invite_hardening.sql, 0021_pages.sql,
│           │                       #    0022_ext_node_kind.sql, 0023_extensions.sql,
│           │                       #    0024_comment_subjects.sql, 0025_dav_tokens.sql,
│           │                       #    0026_page_order.sql, 0027_perf_indexes.sql,
│           │                       #    0028_used_bytes_triggers.sql, 0029_guest_access.sql,
│           │                       #    0030_publications.sql
│           ├── users/              # user service layer (profile)
│           ├── orgsync/            # ✅ Logto sync: client.go (Management API), orgsync.go (membership reconciler, jobs, sweep),
│           │                       #    accounts.go (accounts), spacesync.go (create, rename, Link to Logto)
│           ├── spaces/             # space CRUD, membership, the Admin group, leaving a space
│           │   ├── spaces.go       # RequireAdmin/SetAdmin/AddMember; RevokeMembership is immediate, CompleteDeparture archives what a leaver owned
│           │   ├── avatar.go       # the space's picture in the CAS (upload/clear; the URL is derived from the blob)
│           │   └── permissions_db_test.go # DB-backed; skipped unless PP_TEST_DATABASE_URL is set
│           ├── groups/             # groups + the grants that open folders
│           ├── fs/                 # file tree, versioning, move/copy/rename; quota (EnsureRoom), version retention;
│           │                       #    markdown.go (.md → text/markdown), attachments.go (node_attachments);
│           │                       #    CanContain (fs.go, M43), pages.go (PageTitle, PageTitles), tree.go (Tree, for
│           │                       #    export), copy_pages.go (sub-pages copied with page ids remapped)
│           ├── exthost/            # ✅ packaged-app host: exthost.go (New, registry loaded from the DB, OnCreate seeds, OnCopy, ValidSubject), wire.go,
│           │                       #    discussion.go (comment counts, notify, people search, link resolve), realtime.go,
│           │                       #    packages.go (install/upgrade/remove, seeds, backend secret), apps.go (the shared
│           │                       #    /api/v1/apps/{id}/nodes/{node} runtime), backend.go (proxy, HS256 tokens, /api/apps callback API)
│           ├── apppkg/             # ✅ M51: packaged-app manifest (ParseManifest) and zip reader (Read): limits, content types, digest
│           ├── collab/             # ✅ block-document rooms over ygo: epochs, locks, write-back, reseed, 60 s recheck
│           ├── mdoc/               # ✅ markdown ↔ ProseMirror ↔ Y.XmlFragment; mentions, Go-only comment markers
│           │                       #    (format in mdoc.go); Attachments/Mentions/PlainText/MapAttachments;
│           │                       #    pages.go (subPage/pageLink, doc: → pageLink, PageRefs, MapPageIDs),
│           │                       #    frontmatter.go (icon/cover line editor), emoji.go + emoji.json (dataset);
│           │                       #    testdata/ = golden fixtures
│           ├── blockdoc/           # ✅ a version as a read-only tree, templates/<lang>/*.md; export.go = the export
│           │                       #    and folder zip planner (pages recursive, assets/ per directory)
│           ├── unfurl/             # ✅ SSRF-safe link-preview fetch for the embed block
│           ├── comments/           # ✅ M29 threads on block documents: who may do what, who is told
│           ├── notifications/      # ✅ M29 rows, the per-process SSE broker, notify:mentions job
│           ├── textsearch/         # ✅ M27 folding, query building, snippets (pure)
│           ├── agenttools/         # ✅ M41 the MCP tool registry (REST calls via agenttools.API); anchor.go = comment anchors;
│           │                       #    publish.go = publish_artifact / unpublish_artifact (M50)
│           ├── publish/            # ✅ M50 publications: snapshots, sealed tokens, password grants, Resolve (liveness); crypto.go
│           ├── uploads/            # ✅ resumable upload rows + staging files in <PP_BLOB_ROOT>/.uploads; 24h idle sweep
│           ├── blob/               # CAS Store interface + local and S3 impls; Ingest takes over a staged upload
│           ├── share/              # grants — user, group, link — and the caps they resolve to
│           ├── wopi/               # WOPI host endpoints; access tokens signed with a derived subkey
│           └── preview/            # renderers + job loop; jobs.kind routes, previews.kind is the artifact;
│                                   #    extract.go = extract:text for content search, extract_pdf.go / extract_office.go
│                                   #    = extract:pdf and extract:office (cmd/worker), backfill.go
├── packages/
│   ├── ui/                         # @workspace/ui — shadcn/ui preset (base-ui) + design tokens (globals.css)
│   │   ├── src/components/         # primitives; `sidebar.tsx` owns the drive shell's geometry
│   │   └── src/hooks/              # use-mobile.ts — the sidebar's md breakpoint
│   ├── app-sdk/                    # M51 @pp-hub/app-sdk — packaged apps: frame SDK (connect(), src/index.js), /server (verifyToken), pp-app.schema.json
│   └── utils/                      # shared TS utils (template package, dist via vp pack)
├── deploy/
│   ├── compose/
│   │   ├── compose.dev.yml         # services only — app runs from source; profiles: worker, s3, idp, mail
│   │   ├── compose.prod.yml        # pp-hub, for a VPS / Dokploy; profile: s3
│   │   ├── compose.logto.yml       # self-hosted Logto + its Postgres, deployed beside compose.prod.yml
│   │   ├── .env.dev.example        # → .env (git-ignored), dev values
│   │   ├── .env.prod.example       # → .env (git-ignored), production values
│   │   └── seaweedfs/s3.json       # dev credentials for the S3 profile
│   ├── pp-generate.sh              # writes both stacks, filled envs, ports overrides + Caddyfile to deploy/out/ (git-ignored)
│   ├── logto/pp-logto-setup.sh     # configures a Logto tenant for pp-hub via the Management API
│   ├── lib/pp-env.sh               # env-file helper shared by the two scripts above
│   ├── backup/                     # pp-backup.sh / pp-restore.sh
│   └── weblate/                    # translation platform stack and setup
├── tools/
│   ├── pp-apps/                    # `bun run apps …`: init / pack for packaged apps; templates/
│   └── release/                    # @pp-hub/release — private; carries the product version for Changesets
├── examples/apps/                  # M51 packaged apps to upload (none preinstalled); README (pack and upload)
│   ├── tasks/                      # zero-build task list (plain HTML/JS + a copy of app-sdk.js)
│   ├── kanban/                     # @pp-hub/app-kanban — Kanban (React + Vite + Lingui, pp-app.json 2.0.0, src/position.ts); `bun run apps:build` → dist-pack/kanban-2.0.0.zip
│   └── checklist/                  # @pp-hub/app-checklist — Checklist; `bun run apps:build` → dist-pack/checklist-2.0.0.zip
├── .changeset/                     # pending release notes (bun run changeset); config.json
├── .github/
│   ├── workflows/ci.yml            # web check/test/build, server vet/test/sqlc, image build smoke; also called by release
│   ├── workflows/release.yml       # plan → Version Packages PR | CI → approve → images → tag + GitHub Release
│   ├── workflows/images.yml        # reusable: build + push server/worker images for a version or :edge
│   ├── workflows/edge.yml          # push to main → :edge images
│   └── workflows/i18n.yml          # message catalogs must match the source
├── scripts/version-packages.ts     # the Version Packages PR's commit: version, CHANGELOG, lockfile
├── CHANGELOG.md                    # one section per release, written from the changesets
├── CONTRIBUTING.md                 # workflow, changesets, and the release flow
├── docs/                           # this documentation
└── package.json                    # bun workspaces, Vite+ catalog, the product version
```

## Entry Points

- Web dev: `vp run web#dev` (root `vp run dev`); Vite proxies `/api`, `/auth`, `/s` → `localhost:8080`, `/api` with `ws: true` so the block editor's collab socket goes through the same entry.
- Server local: `cp apps/server/.env.example apps/server/.env`, then fill the OIDC and Logto values with `deploy/logto/pp-logto-setup.sh` (`PP_ENV_FILE=apps/server/.env`; command in the README), then `set -a && source .env && set +a && go run ./cmd/server` from `apps/server`. The binary has no dotenv loader, so the file must be sourced.
- Dev stack: `docker compose -f deploy/compose/compose.dev.yml up -d` — postgres + OnlyOffice at :9980. Profiles add the worker, SeaweedFS, a self-hosted Logto and Mailpit. Full setup: [docs/deployment.md](../deployment.md); production install: [docs/self-hosting.md](../self-hosting.md).

## Notes

- **The space is a URL segment, never state.** Every drive surface is a child
  route of `drive/$spaceId/`: `index.tsx` is the listing, and `home`, `recent`,
  `starred`, `shared` and `trash` sit beside it. So `spaceId` is
  in the params on every one of them, and the rail can read it without a
  fallback. This replaced a `?panel=` search param, which is why Home, Recent
  and Starred used to blank the switcher, the Files tree and the Trash and
  Shared rows: they were siblings of `$spaceId`, so there was no space to read.
  Add a surface as a child route; do not reintroduce a panel param.
- **Settings is one dialog, addressed by the hash.** Seven panes —
  `general|profile|security` about the account, `space|members|groups|danger`
  about the space — live in a single dialog, opened by appending
  `#settings/<pane>` to whatever page you are on. A pane is still a place: it
  survives a reload, is sendable to a colleague and answers the back button.
  What changed is the mechanism, not the invariant. `settings-hash-bridge.tsx`
  is the only thing that opens or closes the modal, and it reads the hash;
  every exit is a navigation, never `SettingsModal.hide()`. The space band is
  filtered out for a non-admin and a deep link to one of its panes is replaced
  with the default pane rather than answered — the same reasoning that makes
  the `/admin` layout route redirect in `beforeLoad`: a surface that resolves
  has already told the caller it exists and what it is for.
- **Safety lives on the route group, not in the handler.** `httpapi/server.go`
  puts the rate limiter, the same-origin check and authentication on
  `/api/v1` with `r.Use`, in that order, and a handler calls `caller(r)`
  without re-checking anything. This is deliberate: there were once seventy-odd
  handlers and thirty-odd hand-written guards, and nothing but eyesight
  connected the two numbers. `routes_test.go` walks the router and fails if an
  `/api/v1` route is missing either middleware — and fails if `/wopi/*` gains
  the origin check, which would break the Document Server's own saves. A route
  that must be public is registered outside the group, never exempted inside
  it.
- **Drive shell layout.** `routes/drive.tsx` is the shadcn dashboard shell:
  `SidebarProvider` (`--sidebar-width: 16rem`, `--header-height: 3rem`) holding
  `app-sidebar` (`variant="inset"`, `collapsible="offcanvas"`) beside a
  `SidebarInset`. The rail is `space-switcher` in `SidebarHeader`, then
  `nav-create` + `nav-space` in `SidebarContent`, then `storage-meter` +
  `nav-user` in `SidebarFooter`. Row geometry comes from `SidebarMenuButton`
  and `SidebarMenuSubButton` — there is no `railRow()` any more, and a new row
  must not reintroduce one.
  Two things about this are load-bearing and easy to break:
  - `SidebarProvider` is only `min-h-svh`, so `drive.tsx` adds
    `h-svh overflow-hidden` and `SidebarInset` adds `min-h-0 overflow-hidden`.
    Without them the listing grows the page instead of its own scroller, and
    the marquee band ends up measured against the document.
  - the inset's margin and rounding are `peer-data-[variant=inset]`, which only
    matches while `SidebarInset` is a **direct sibling** of the sidebar. Any new
    provider (`HeaderSlotProvider` is one) wraps `SidebarProvider`, never sits
    between it and the inset.
    The rail reads its space through `useActiveSpaceId()`, which is the route
    param on every drive surface and the last space opened at `/drive` itself.
    The settings dialog uses the same hook: it renders from the modal
    container at the router root, so it has no params of its own.
- **One header, filled by the route.** `site-header.tsx` is rendered once by
  the shell: the `SidebarTrigger`, a breadcrumb derived from the pathname plus
  the `dirs` cache, then `nav-search` and a `HeaderSlot`. A surface no longer
  renders a header row of its own — `home.tsx` and the old account layout each
  used to add a second 40px band under it. Controls that belong to one route go
  through `header-slot.tsx`: the shell publishes the slot node, the route
  portals into it (`ListingControls` from the listing, "Browse files" from
  Home). That is why the header never has to sniff the pathname to decide
  whether there are rows to sort. Deliberately not `ui-store`: that store is
  for ephemeral UI _state_, and a ReactNode is not state.
- **Two rhythms, on purpose.** Every surface except the folder listing renders
  inside `surface.tsx` — `@container/main` plus `p-4` at every breakpoint.
  Put the rhythm there, not in the route: ten surfaces retyping
  the class string is how it drifts. The listing keeps its own scroller
  (`rounded-b-xl overflow-hidden`, so rows clip to the inset's corner, and a
  sticky header), but its grid and list import `LISTING_PADDING` from
  `surface.tsx` so their edges match Home, Recent, Starred, Shared and Trash.
- **Folder navigation never shows another folder's rows.** `childrenOptions`
  has no `placeholderData`. A cached folder renders its own rows at once and
  refetches behind a `FetchingBar`; an uncached one shows `NodeGridSkeleton`
  / `NodeListSkeleton` (see Loading States in frontend-conventions). `FolderTreeItem` also queries its
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
- **Members is one table, and the only membership surface.** `people-table.tsx`
  owns members, an Admin badge, the group chips and a **Pending handover**
  section fed by `GET /spaces/{id}/departures`. Rows stay one line high: at
  most two group chips, the rest folded into `+N` (full list in the tooltip).
  Every membership write — make/remove admin, add to/remove from a group,
  remove from space — lives in the row's trailing ⋯ menu, and removal goes
  through `confirm-dialog.tsx`; `group-list.tsx` owns the
  groups, now a pane of its own beside Members rather than a 20rem drawer
  glued to it — filtering the table by group and administering the groups are
  different questions. `invite-modal.tsx` is the only way into a space —
  now an email address and the admin toggle, since `POST /spaces/{id}/members`
  takes `{email, as_admin}` and membership itself is decided in Logto. Admin
  _is_ membership of the automatic `Admin`
  group, so the menu item and the chip are one fact — which is why all three
  membership mutations invalidate `["spaces", id]` rather than just the member
  list, and why that group is read-only in the drawer. What a group _opens_ is
  still set on the folder, in its access dialog: this table answers who is in
  the group, the folder answers what the group reaches. The departures query is
  admin-gated server-side, so it is `enabled` on the admin check rather than
  firing and 403-ing, and **Complete handover** goes through
  `confirm-dialog.tsx` because it is irreversible — revocation already
  happened, this is the teardown. See
  [permissions.md](../architecture/permissions.md).
- **Node menu actions have one definition.** `node-actions.tsx` builds the
  action list; the list view, grid view and their right-click menus all render
  from it. Add an action there, not in a menu.
- **One search, and one dead store field.** `nav-search.tsx` sits in the header
  and is a _button_ through to `command-palette.tsx` — a server search across
  every space — not a field. It is icon-only below `sm` so the breadcrumb keeps
  its width on a phone.
  `ui-store.filterQuery` is the leftover of a rail filter field that no longer
  exists: `drive/$spaceId/index.tsx` still reads it (and still has a "No
  matching files or folders" empty state behind it), but nothing writes it, so
  it is permanently `""`. Either bring the field back or delete the field and
  that empty-state branch together — do not add a second writer somewhere else.
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
  automatic `Admin` group — administers one space (the Space band of the
  settings dialog, `#settings/space|members|groups|danger`); a
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
  The tray does not invalidate per event: `lib/upload-invalidation.ts`
  collects the touched folders and trees and refetches them at most once per
  `UPLOAD_REFRESH_MS` (a fixed window, so a long queue still refreshes every
  second), and refetches `GET /spaces` (the storage meter) only once nothing
  is still uploading.
- **Folder listings are virtualized** (`@tanstack/react-virtual`, scrolled by
  the route's `listingRef`). `NodeList` keeps spacer rows above and below the
  rendered `<tr>`s; `NodeGrid` lays out label and card rows from
  `lib/listing-geometry.ts`, whose constants mirror the Tailwind sizes
  (`h-12` folder cards, `h-[198px]` file cards, `gap-3`, `gap-6`) — change
  one and change the other. Off-screen rows are not in the DOM, so marquee
  selection hit-tests geometry (`hitTestList`/`hitTestGrid`) instead of
  measuring `[data-node-id]` boxes.
  The tus client's `PATCH` (`lib/api/tus.ts`) is the one request that uses
  `XMLHttpRequest` rather than the shared `ky` client, because `fetch`
  reports no upload progress. Before M30 that was `uploadFileWithProgress`,
  a single multipart POST; now a file goes up in chunks sized from measured
  throughput, and the store keeps each transfer's upload URL so pause, resume
  and retry continue from the server's offset instead of byte zero.
- The Go module lives inside the JS monorepo but is independent of bun workspaces.
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen is deferred until the API surface justifies it.
