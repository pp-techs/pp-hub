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
- **A settings pane is an address, not component state.** The whole app has
  one settings dialog, and which pane it shows is `#settings/<slug>` in the
  URL — nothing else. A pane has to survive a reload, be sendable to a
  colleague and answer the back button, so the router owns it; a second copy
  in component state could disagree. This used to be a route per pane under
  `/drive/account` and `/drive/$spaceId/settings`; the mechanism changed, the
  invariant did not.
- **The settings dialog never closes itself.** `settings-hash-bridge.tsx`
  (mounted in `drive.tsx`) is the only caller of `SettingsModal.show/hide`,
  and it reads the hash. Every exit inside the dialog is a navigation —
  `closeSettings()` strips the fragment, the bridge then hides the modal.
  Calling `modal.hide()` directly leaves an address that still names a pane,
  and the next reload reopens it.
- **The dialog's nav is plain markup, not the `Sidebar` primitive.**
  `ModalContainer` sits at the router root, outside the drive shell's
  `SidebarProvider`, so `Sidebar`/`SidebarMenuButton` would throw on
  `useSidebar()`. Nesting a second provider is worse: it registers a second
  ⌘B handler that toggles the rail behind the dialog.
- **Gate by filtering the pane list, not by rendering a refusal.** The Space
  band disappears for a non-admin, and a deep link to one of its panes is
  replaced with the default pane. An admin-only _page_ redirects in
  `beforeLoad` with a toast, for the same reason: rendering "only an admin
  can…" at an address that already resolved tells the caller a surface exists
  where it does not.
- **Ephemeral view state belongs in the URL when it is shareable** (`?dir=`,
  `?view=`, `?sort=`) and in component state or `ui-store` when it is not.
  The member table's group filter used to be `?group=`; inside a dialog whose
  address already names the pane, filtering is a glance rather than a place,
  so it is plain `useState` now.

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
  paste item never gets a second definition in a second menu. An item that
  names the file it creates wears that file's iconify `vscode-icons` glyph
  (`extIcon` in `components/node-icon.tsx`), never a Lucide stand-in.
- **A `shortcut` badge is a contract.** `nodeActions` names the key, the
  listing binds it — the folder route's key handler dispatches by action key
  through `nodeActions` itself, so the badge and the capability gates cannot
  drift apart. No badge ships without a binding: ⌘D is the browser's bookmark
  key and deliberately has none. In the block editor, ⌘K opens the link
  popover (`linkShortcut` in `lib/collab/mdoc-kit.ts`); the mark keys come
  with the Tiptap marks.
- **`packages/ui` is shadcn's base-ui variant** (style `base-mira`,
  `@base-ui/react`). Composition uses `render={<El/>}`, never `asChild`.

## Loading States

- **A view never borrows another resource's data while it loads.**
  `placeholderData: prev` is for refining the _same_ query on the _same_
  surface: a search term, a page of the security log. It is not for moving
  between folders or spaces. The folder listing used it that way: folder A's rows
  stayed on screen as folder B, still clickable and feeding the details panel
  and breadcrumb, then swapped out. That swap was the flicker it meant to hide.
- **Cached means instant, uncached means skeleton.** Without a placeholder,
  TanStack Query already renders a cached key's own data at once and
  refetches behind it; `isPending` is only true when there is nothing to show.
  The listing marks a background refetch with `FetchingBar`
  (`isFetching && !isPending`), which takes no height.
- **A `beforeLoad` guard never waits on the network for data it already has.**
  The router keeps the previous matches, and so the previous folder, on
  screen until every `beforeLoad` in the tree settles, and `/drive`'s runs on
  every navigation, `?dir=` changes included. `requireAuth` therefore uses
  `ensureQueryData({ revalidateIfStale: true })`: the first call waits, later
  ones answer from cache and revalidate in the background. A revoked session
  still surfaces through the global 401 handler. This is not `staleTime:
Infinity`; the identity is refetched on every navigation, just not awaited.
- **A loading state has the shape of what it replaces.** Skeletons come from
  `components/loading.tsx` (`LoadingStatus`, `SkeletonTableRows`,
  `FetchingBar`), or sit next to the component they stand
  in for when they share its layout constants (`NodeGridSkeleton`,
  `NodeListSkeleton`). A table keeps its real header and puts skeleton rows in
  its body. A bare "Loading…" line is not a loading state: the content then
  lands at another height and moves everything below it.
- Custom skeleton markup is wrapped in `LoadingStatus`, which carries the one
  `role="status"` and the screen-reader text.

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
- **One identity endpoint.** `GET /api/v1/users/me`, cached under `keys.me`, through `useMe()` / `fetchMe()` / `requireAuth()` in `queries/auth.ts`. The thin second "who am I" route it replaced meant the rail and the profile page held two caches of the same person, free to disagree. Never add a second one back.
- **`staleTime: Infinity` on the identity query is forbidden.** The session is a row in the database, so it can be revoked from another device or expire under a tab that is still open. A never-stale identity is exactly how a dead session went unnoticed until the next write failed.
- **401 is handled once, globally.** `api/client.ts` cannot navigate — the router does not exist at transport level — so it calls the handler registered through `setUnauthorizedHandler` in `main.tsx`, which clears the query cache and routes to `/login?redirect=<path>`; it still throws `ApiError(401)` so route loaders can branch. Two guards are load-bearing: `/login` is excluded, because its own `fetchMe` probe legitimately 401s for a signed-out visitor and answering that with a navigation to `/login` is a loop, and a `navigating` latch keeps a burst of parallel 401s from stacking one navigation per failed query. A surface never writes its own 401 branch.
- **Sign-out goes through `signOut(qc)`** (`queries/auth.ts`), never an inline `logout()` call. It returns `true` when it has started a navigation off this origin — the provider advertised `end_session_url`, and skipping the IdP hands the same account straight back on the next sign-in — and `false` when sign-out finished here and the caller should route to `/login` itself. The ordering inside is not incidental: on the branch that stays put the query cache is cleared, because cached data outliving a sign-out is somebody else's data on a shared machine; on the branch that leaves, the unauthorized handler is disabled first and the cache is left alone, since a mounted query refetching into the just-revoked session would otherwise race a router navigation to `/login` against the cross-origin assignment and win — landing on the sign-in page with the IdP session still standing.
- Auth transport: same-origin httpOnly cookie session; the SPA never stores tokens in localStorage and never reads the session cookie.

## Localization

Lingui + gettext `.po`, English and Vietnamese. Full rationale and the Weblate
wiring are in [i18n.md](../architecture/i18n.md); these are the rules that
bind day-to-day code.

- **JSX text is `<Trans>`** (`@lingui/react/macro`). Attributes, toasts and
  plain variables use `const { t } = useLingui()` and the `` t`…` `` template
  tag — never `t("…")`, which is not the macro.
- **A module-scope constant holds `MessageDescriptor`s, never rendered text.**
  Module scope runs once at import time, before a catalog exists, so a
  rendered label there freezes whichever language loaded first and never
  follows a switch. Build them with `msg` (`@lingui/core/macro`) and resolve
  at render with `i18n._(descriptor)`. Keep the `Record<Union, …>` annotation
  so exhaustiveness still checks. This is the single most common way a string
  silently stops translating, which is why `lingui/t-call-in-function` is an
  error rather than a warning.
  A list built _inside_ a component is not module scope — pass plain
  translated strings there and skip the indirection.
- **A sentence is one message.** Never splice translated fragments: word
  order, conjunctions and agreement are not English's to lend. The activity
  feed used to read `{actor} {verb} {node} in {space}` from a verb map; it is
  now one complete message per `ActivityKind`.
- **Counts use ICU**, `<Plural>` or `plural()` — never
  `` `${n} item${n === 1 ? "" : "s"}` ``. Vietnamese has only the `other`
  category; do not "fix" a `vi` plural by adding `one`.
- **Route `head` titles use `pageTitle(msg\`…\`)`** from `@/lib/i18n`. `head`runs outside React, so it reaches the singleton rather than a hook, and
titles follow the language on the next navigation rather than instantly.`pageTitle`appends`· pp-hub` itself.
- **Formatting goes through `useFormatters()`** (`@/lib/format`), which binds
  the active locale and re-renders on a switch. `lib/api/` takes an optional
  trailing `locale` argument and never imports the i18n runtime — the
  dependency points one way, the same way components never call `api`
  directly.
- **`packages/ui` never imports an i18n library.** A design-system package
  cannot know which one its consumer picked. The primitives read their
  screen-reader labels from `UiStringsProvider`
  (`@workspace/ui/lib/strings`), whose defaults are English, and the app fills
  it in from `LocaleProvider`.
- **Language is the one account-level preference.** It is a fact about a
  person, so it lives on `users.settings` and rides in on `GET /users/me`;
  theme and the default listing stay device-local because they are facts about
  a machine. Every applied locale is also mirrored to `localStorage`, and that
  echo is what the first paint reads — the account answer arrives one round
  trip too late to avoid a flash.
- **Never translate**: the product name, file extensions, MIME types, HTTP
  methods, URL paths, cache keys, CSS classes, `data-*` values, browser and
  platform names, developer-facing `Error`/`console` text, or the language
  names in `LOCALES` — those are written in the language they name.
- After adding strings: `bun run i18n:extract` and `bun run i18n:template`
  from `apps/web`, and commit the catalogs.

## Style

- Formatting/linting: Oxlint + Oxfmt via `vp check` — do not add competing formatters.
- TypeScript strict mode (see `tsconfig.json`); no `any` in committed code.
