Web app (`apps/web`): React 19 + TanStack Router (file-based routes via `@tanstack/router-plugin`, routes in `src/routes/`) + TanStack Query (server state) + zustand (`src/lib/ui-store.ts`, ephemeral UI state only). UI primitives come from `@workspace/ui` (`packages/ui`), a shadcn/ui preset; add components with `bunx shadcn add <name>` from `apps/web`.

## Data Layer (`src/lib/`)

Three layers, one direction only — components never call `api` directly:

1. **`api/`** — transport. `client.ts` is a single ky instance (same-origin cookies, `throwHttpErrors: false`, 401 → `ApiError`); `types.ts` hand-typed interfaces matching the server; `endpoints/index.ts` one typed async function per API call; `utils.ts` shared helpers (`fmtSize`, `isOfficeFile`, `previewKind`, `openEditor` — opens the `/edit/{nodeId}` full-page editor in a new tab). `index.ts` re-exports everything — import from `@/lib/api`.
2. **`queries/`** — TanStack Query hooks, one file per resource (`auth.ts`, `spaces.ts`, `nodes.ts`, `shares.ts`). `keys.ts` is the single source of truth for cache keys. Route guards (`requireAuth`, `fetchMe`) live in `queries/auth.ts` and take the `QueryClient`.
3. **`ui-store.ts`** — zustand for ephemeral UI state only (selection, panels); never server state.

## Routes

- **A space is a URL segment.** Anything about one space is a child route of
  `routes/drive/$spaceId/`, so `spaceId` is in the params on every drive
  surface and the rail never has to guess. Never reintroduce a `?panel=`-style
  search param for a whole surface: that is what made Home, Recent and Starred
  blank the rail.
- **A settings tab is a route**, rendered through `settings-tabs.tsx`. Panes
  have to survive a reload, be sendable to a colleague and answer the back
  button, so the router owns which one is open — never component state.
- **Gate in `beforeLoad`, not in the pane.** An admin-only page redirects with
  a toast; rendering "only an admin can…" at a URL that already resolved tells
  the caller a page exists where it does not.
- **Ephemeral view state belongs in the URL when it is shareable** (`?dir=`,
  `?group=`, `?view=`) and in `ui-store` only when it is not (selection, open
  panels).

## Drive Shell

- **The shell is the shadcn dashboard shell.** `routes/drive.tsx` = `SidebarProvider`
  → `app-sidebar` (`variant="inset"`, `collapsible="offcanvas"`) + `SidebarInset`
  → `site-header` + `<Outlet/>`. `--sidebar-width: 18rem`, `--header-height: 3rem`.
- **A rail row is a `SidebarMenuButton`**, a tree row a `SidebarMenuSubButton`.
  `railRow()` is gone; do not reintroduce a parallel row helper.
  Skip the `tooltip` prop — with `offcanvas` the sidebar never reaches the
  collapsed state that would show it.
- **`SidebarInset` must stay a direct sibling of the sidebar.** Its margin and
  rounding are `peer-data-[variant=inset]`. A new provider wraps
  `SidebarProvider`; it never sits between the provider and the inset.
- **The shell pins the viewport.** `SidebarProvider` is only `min-h-svh`, so
  `drive.tsx` adds `h-svh overflow-hidden` and `SidebarInset` adds
  `min-h-0 overflow-hidden`. Without both, the listing grows the page rather
  than its own scroller and the marquee band measures against the document.
- **There is exactly one header.** A surface never renders a header row; it
  puts its controls in the shell's header through `HeaderActions`
  (`header-slot.tsx`). Titles come from the pathname via `SECTIONS` in
  `site-header.tsx` — add a segment there, not a prop.
- **Page rhythm lives in `surface.tsx`.** Every surface except the folder
  listing renders inside `<Surface>`; size sections with container queries
  (`@xl/main`, `@4xl/main`), not viewport breakpoints, because the details
  panel narrows the column without the route knowing. The listing stays
  full-bleed on purpose.
- **Where a write lands is derived from the URL, once, by `DriveWriteProvider`**
  (`components/drive-write.tsx`), mounted in `routes/drive.tsx` above the
  sidebar and the inset alike. The rail's New menu and the listing's
  background context menu both call `useDriveWrite()` rather than each
  guessing the current folder — they used to disagree, and the rail uploaded
  into the space root from inside a folder. `folderActions()`
  (`components/folder-actions.tsx`) is the single list of what a folder can
  be asked to do, exactly as `nodeActions()` is for a node; a create/upload/
  paste item never gets a second definition in a second menu.
- **`packages/ui` is shadcn's base-ui variant** (style `base-mira`,
  `@base-ui/react`). Composition uses `render={<El/>}`, never `asChild`.

## Monorepo Rules

- Workspace layout: `apps/*`, `packages/*`, `tools/*` (see root `package.json`).
- Use Vite+ built-ins: `vp dev`, `vp build`, `vp check`, `vp test`. Scripts cannot shadow built-ins — check `package.json` / `vite.config.ts` and use `vp run <name>` for project scripts/tasks.
- After pulling remote changes: `vp install`. Before pushing: `vp check` + `vp test`.
- A package with tests needs a `test` script in its `package.json`, or
  `vp run -r test` skips it silently and the gate means nothing.
- Dependency versions shared across workspaces go through the `catalog` in root `package.json`.

## Validation Workflow

```bash
vp run ready    # vp check && tests && build for the whole monorepo
```

## Preview URLs

- Ask for a preview **kind** (`thumbnail` / `text`) — the artifact — never the
  job kind the server used to produce it. `previewKind` no longer mirrors
  anything in Go; images, PDFs, video and office documents all answer
  `thumbnail`.
- Choose a viewer by **MIME**, not preview kind. The preview kind cannot tell
  `<img>` from `<object>` from `<video>`, because all three store a thumbnail.
- Always build preview URLs with `previewUrl(node)`. The response is cached
  `immutable` for a year, so the version in the query string is the only thing
  that busts it; a hand-written URL pins a stale thumbnail in every viewer's
  cache after an edit.
- `hasThumbnail` gates the image; fall back to `NodeIcon`, never a blank frame.
  A missing thumbnail is an ordinary state — the file may have no renderer, or
  no worker may be deployed.

## API Contract

- Types live in `apps/web/src/lib/api/types.ts` (hand-typed); OpenAPI codegen is deferred until the surface grows. Keep interfaces in sync with the Go handlers.
- Auth: same-origin httpOnly cookie session; the SPA never stores tokens in localStorage.

## Style

- Formatting/linting: Oxlint + Oxfmt via `vp check` — do not add competing formatters.
- TypeScript strict mode (see `tsconfig.json`); no `any` in committed code.
