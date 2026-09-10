Web app (`apps/web`): React 19 + TanStack Router (file-based routes via `@tanstack/router-plugin`, routes in `src/routes/`) + TanStack Query (server state, `src/lib/queries/` — one file per resource) + zustand (`src/lib/ui-store.ts`). UI components come from `@workspace/ui` (`packages/ui`), a shadcn/ui preset; add components with `bunx shadcn add <name>` from `apps/web`.

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

- API types come from `packages/api-client`, generated from the server's `openapi.yaml` (generated client starts in M2 when the API surface grows; M1's single `/me` endpoint is hand-typed). Never hand-edit generated files.
- Auth: same-origin httpOnly cookie session; the SPA never stores tokens in localStorage.

## Style

- Formatting/linting: Oxlint + Oxfmt via `vp check` — do not add competing formatters.
- TypeScript strict mode (see `tsconfig.json`); no `any` in committed code.
