# pp-hub

A self-hosted workspace for a company: file storage, collaborative editing of
Word/Excel/PowerPoint documents, and fine-grained sharing — on your own server.

- **Drive** — folders, uploads of any size (resumable), versions, trash,
  search, previews and thumbnails.
- **Office** — open and co-edit documents in the browser (OnlyOffice).
- **Spaces** — one per company or department, each with its own people,
  groups and storage quota.
- **Sharing** — to people, to groups, or by public link; nothing is visible
  until it is shared.
- **Accounts** — sign-in, invitations and suspension through
  [Logto](https://logto.io), self-hosted alongside or on Logto Cloud.
- **Admin** — security log with CSV export, alerts, instance settings, backups.

English and Vietnamese UI.

## Run it on a server

pp-hub runs as two Docker Compose stacks — Logto and pp-hub — on any host
with Docker, behind a reverse proxy or a platform that routes domains. Two
scripts do the setup:

```sh
./deploy/pp-generate.sh             # compose files, environments with secrets, Caddy config
./deploy/logto/pp-logto-setup.sh    # Logto apps, roles, email, sign-up, webhook
```

Follow **[docs/self-hosting.md](docs/self-hosting.md)** — about 30 minutes
from a fresh server to the first invitation.
[docs/deployment.md](docs/deployment.md) is the full reference.

## Develop

Requirements: [bun](https://bun.sh), Go, Docker, and the
[Vite+](https://viteplus.dev) `vp` CLI.

```sh
# Postgres, OnlyOffice, a local Logto and a mail catcher (http://localhost:8025)
docker compose -f deploy/compose/compose.dev.yml --profile idp --profile mail up -d

# Configure the local Logto once. In its console (http://localhost:3002) create
# the admin account, then a Machine-to-machine app with the role
# "Logto Management API access", and run:
cp apps/server/.env.example apps/server/.env
LOGTO_ENDPOINT=http://localhost:3001 \
LOGTO_BOOTSTRAP_CLIENT_ID=<id> LOGTO_BOOTSTRAP_CLIENT_SECRET=<secret> \
PP_EXTERNAL_URL=http://localhost:5173 \
SMTP_HOST=host.docker.internal SMTP_PORT=1025 SMTP_FROM='pp-hub <dev@localhost>' \
PP_ENV_FILE=apps/server/.env \
./deploy/logto/pp-logto-setup.sh

# API server (reads real environment variables, so source the file)
cd apps/server && set -a && . ./.env && set +a && go run ./cmd/server

# Web app, from the repository root: http://localhost:5173
vp run dev
```

Set `PP_BOOTSTRAP_ADMIN_EMAIL` in `apps/server/.env` to your address before
the first sign-in to become the instance administrator.

| Task                           | Command                                                                                            |
| ------------------------------ | -------------------------------------------------------------------------------------------------- |
| Format, lint, type-check       | `vp check`                                                                                         |
| Web tests                      | `vp test`                                                                                          |
| Server tests (with a database) | `cd apps/server && PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./...` |
| Update translation catalogs    | `vp run web#i18n:extract`                                                                          |

## Documentation

Start at [docs/SUMMARY.md](docs/SUMMARY.md): architecture, data model,
permissions, the Logto integration, code standards and the roadmap.
