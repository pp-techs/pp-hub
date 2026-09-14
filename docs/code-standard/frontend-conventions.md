Web app (`apps/web`): React 19 + TanStack Router (file-based routes via `@tanstack/router-plugin`, routes in `src/routes/`) + TanStack Query (server state) + zustand (`src/lib/ui-store.ts`, ephemeral UI state only). UI primitives come from `@workspace/ui` (`packages/ui`), a shadcn/ui preset; add components with `bunx shadcn add <name>` from `apps/web`.

## Data Layer (`src/lib/`)

Three layers, one direction only — components never call `api` directly:

1. **`api/`** — transport. `client.ts` is a single ky instance (same-origin cookies, `throwHttpErrors: false`, 401 → `ApiError`); `types.ts` hand-typed interfaces matching the server; `endpoints/index.ts` one typed async function per API call; `utils.ts` shared helpers (`fmtSize`, `isOfficeFile`, `previewKind`, `openEditor` — opens the `/edit/{nodeId}` full-page editor in a new tab). `index.ts` re-exports everything — import from `@/lib/api`.
2. **`queries/`** — TanStack Query hooks, one file per resource (`auth.ts`, `spaces.ts`, `nodes.ts`, `shares.ts`). `keys.ts` is the single source of truth for cache keys. Route guards (`requireAuth`, `fetchMe`) live in `queries/auth.ts` and take the `QueryClient`.
3. **`ui-store.ts`** — zustand for ephemeral UI state only (selection, panels); never server state.

## Monorepo Rules

- Workspace layout: `apps/*`, `packages/*`, `tools/*` (see root `package.json`).
- Use Vite+ built-ins: `vp dev`, `vp build`, `vp check`, `vp test`. Scripts cannot shadow built-ins — check `package.json` / `vite.config.ts` and use `vp run <name>` for project scripts/tasks.
- After pulling remote changes: `vp install`. Before pushing: `vp check` + `vp test`.
- Dependency versions shared across workspaces go through the `catalog` in root `package.json`.

## Validation Workflow

```bash
vp run ready    # vp check && tests && build for the whole monorepo
```

## API Contract

- Types live in `apps/web/src/lib/api/types.ts` (hand-typed); OpenAPI codegen is deferred until the surface grows. Keep interfaces in sync with the Go handlers.
- Auth: same-origin httpOnly cookie session; the SPA never stores tokens in localStorage.

## Style

- Formatting/linting: Oxlint + Oxfmt via `vp check` — do not add competing formatters.
- TypeScript strict mode (see `tsconfig.json`); no `any` in committed code.
