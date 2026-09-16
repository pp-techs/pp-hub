# pp-hub

A minimal, self-hosted Google Workspace alternative: Drive, collaborative office editing (OnlyOffice/WOPI), permissions, and multiple spaces.

Monorepo: bun workspaces + Vite+ (frontend), Go backend (planned, `apps/server`), Postgres + content-addressed storage, docker-compose all-in-one deployment.

## Development

- Check everything is ready:

```bash
vp run ready
```

- Run the tests:

```bash
vp run -r test
```

- Build the monorepo:

```bash
vp run -r build
```

- Run the development server:

```bash
vp run dev
```

## Full Stack (M1)

```bash
docker compose -f deploy/compose/compose.dev.yml up -d
```

Identity provider: Logto — create a free Cloud tenant at <https://cloud.logto.io> (Traditional Web app, redirect URI `http://localhost:8080/auth/callback`) or self-host with `--profile idp`. OnlyOffice on :9980, API server on <http://localhost:8080>. Setup steps: [docs/deployment.md](docs/deployment.md). Then run `vp run dev` and open <http://localhost:5173> to sign in.

## Documentation

See [docs/SUMMARY.md](docs/SUMMARY.md) — architecture, codebase layout, code standards, and product requirements.
