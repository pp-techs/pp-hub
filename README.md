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

## Full Stack

```bash
# Services the app talks to: postgres + OnlyOffice.
# Profiles add more: --profile worker (PDF/video/office thumbnails),
#                    --profile s3 (SeaweedFS), --profile idp (self-hosted Logto)
docker compose -f deploy/compose/compose.dev.yml up -d

# API server, from source. It reads real env vars, so .env must be sourced.
cd apps/server && cp .env.example .env    # once, then fill in PP_OIDC_*
set -a && source .env && set +a && go run ./cmd/server

vp run dev                                # web, http://localhost:5173
```

Identity provider: Logto — create a free Cloud tenant at <https://cloud.logto.io> (Traditional Web app, redirect URI `http://localhost:5173/auth/callback`) or self-host with `--profile idp` (admin console on <http://localhost:3002>). OnlyOffice on :9980, API server on <http://localhost:8080>.

Deploying to a server or Dokploy: [docs/deployment.md](docs/deployment.md).

## Documentation

See [docs/SUMMARY.md](docs/SUMMARY.md) — architecture, codebase layout, code standards, and product requirements.
