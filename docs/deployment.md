# Deployment

Two compose files, two env files, one convention.

| Path                          | Committed?  | What it is                             |
| ----------------------------- | ----------- | -------------------------------------- |
| `apps/server/.env.example`    | yes         | Template for local development         |
| `apps/server/.env`            | no, ignored | Your local values — **source it**      |
| `deploy/compose/.env.example` | yes         | Template for production                |
| `deploy/compose/.env`         | no, ignored | Production values — compose auto-reads |

The rule is the one `.gitignore` already encodes: **`.env.example` is
committed, `.env` is yours**. Each lives beside the thing that reads it. There
is no `server.env` and no `.env.local` any more — those were the same idea
under three names.

They are separate files because they hold genuinely different values: the dev
server runs on your host and talks to `localhost:5432`, while the production
server runs in compose and talks to `postgres:5432`. One file could never serve
both.

Two compose files, for two different jobs:

| File                              | What it runs                                     | Use it when                                 |
| --------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| `deploy/compose/compose.prod.yml` | Everything, built from this repo                 | Deploying to a VPS or Dokploy               |
| `deploy/compose/compose.dev.yml`  | Only the things the app talks to — no app images | Developing, running Go and Vite from source |

## The one constraint that shapes everything

**The SPA and the API must be served from the same origin.** This is not a
preference:

- the session cookie is host-only (no `Domain`) with `SameSite=Lax`
  (`internal/auth/auth.go`), and
- the API client sets `credentials: "same-origin"`
  (`apps/web/src/lib/api/client.ts`).

Put the bundle on `app.example.com` and the API on `api.example.com` and the
cookie is never sent: every request is a 401 and nobody can log in.

The Go server serves **only** `/healthz`, `/auth/*`, `/api/v1/*`, `/wopi/*` and
`/s/*` — it has no static file handler. So something has to serve the bundle
and proxy the API on one hostname. That is what the `web` container is:
nginx with the built SPA and a proxy to `server:8080`
(`apps/web/Dockerfile`, `apps/web/nginx.conf`).

```
                    ┌──────────────────────────────────────┐
 drive.example.com ─┤ web (nginx)                          │
                    │   /            → SPA bundle          │
                    │   /api /auth /wopi /s → server:8080  │
                    └───────────────────┬──────────────────┘
                                        │
                              ┌─────────▼────────┐   ┌──────────┐
                              │ server           ├───┤ postgres │
                              └─────────┬────────┘   └────┬─────┘
                                        │                 │
                              ┌─────────▼────────┐        │
                              │ worker (optional)├────────┘
                              └─────────┬────────┘
                                        │ conversion
 office.example.com ──────────► onlyoffice
```

`nginx.conf` mirrors the dev proxy in `apps/web/vite.config.ts`. **Change one,
change the other.**

### Why OnlyOffice needs its own hostname

`PP_ONLYOFFICE_URL` goes into the editor `iframe` the browser loads, so it has
to be a public address — an internal service name will not do. Give it a second
domain. It is not proxied through `web` because serving Document Server under a
sub-path is fragile.

### The `/s/<token>` collision

One path, two meanings: the server answers it as JSON (`handleShareInfo`) and
the SPA has a page at the same URL. They are told apart by `Accept`:

| Request                                   | Goes to      |
| ----------------------------------------- | ------------ |
| `GET /s/<token>` with `Accept: text/html` | SPA          |
| `GET /s/<token>` with anything else       | API          |
| `GET /s/<token>/download`, `/children`, … | API (always) |

Any reverse proxy you put in front must preserve this, or share links break for
anonymous visitors.

## Production

```bash
cp deploy/compose/.env.prod.example deploy/compose/.env   # then fill it in
docker compose -f deploy/compose/compose.prod.yml up -d --build
```

Required values have no defaults — compose refuses to start and names the
missing variable rather than booting a half-configured stack.

Generate the session key once and keep it:

```bash
openssl rand -base64 32
```

`PP_SESSION_KEY` is shared by the server and the worker. The worker signs
office-conversion tokens that the _server_ verifies, so a mismatch means office
thumbnails silently never appear. Changing it logs everyone out.

Register with your OIDC provider:

- redirect URI: `https://drive.example.com/auth/callback`
- post-logout redirect: `https://drive.example.com`

### Dokploy

Dokploy's recommended flow is a **clean compose file** — no Traefik labels, no
`dokploy-network` — with domains assigned in the UI. `compose.prod.yml` is
written that way: it publishes no host ports and uses `expose` only, since
binding 80/443 would collide with Dokploy's Traefik.

1. Create a Compose application pointing at this repo.
2. Paste or reference `deploy/compose/compose.prod.yml`.
3. Put the contents of `.env.prod.example` into the Environment tab, filled in.
4. In the Domains tab add two domains:

   | Service      | Container port | Example domain       |
   | ------------ | -------------- | -------------------- |
   | `web`        | 80             | `drive.example.com`  |
   | `onlyoffice` | 80             | `office.example.com` |

5. Deploy, then run the backfill once (below).

Do not bind ports 80 or 443 in the compose file — they belong to Traefik.

### Plain VPS

Uncomment the `ports:` blocks marked `PLAIN VPS` and point your own reverse
proxy (Caddy, nginx, Traefik) at them. Terminate TLS there.

### After the first deploy

Previews are only enqueued on upload, so files that predate the worker have no
thumbnail until you sweep once:

```bash
docker compose -f deploy/compose/compose.prod.yml run --rm worker --backfill
```

Safe to re-run: it skips versions that already have a preview or an in-flight
job.

## Development

The app itself runs from source; compose provides only what it talks to.

```bash
docker compose -f deploy/compose/compose.dev.yml up -d

cd apps/server
cp .env.example .env          # once, then fill in PP_OIDC_*
set -a && source .env && set +a
go run ./cmd/server

vp run dev                    # from the repo root, http://localhost:5173
```

The server has no dotenv loader — it reads real environment variables — so
`.env` has to be **sourced**, not merely present.

Optional pieces are behind profiles, so the default `up` stays light:

| Profile  | Adds                     | For                                    |
| -------- | ------------------------ | -------------------------------------- |
| _(none)_ | postgres, onlyoffice     | Everyday work                          |
| `worker` | preview worker           | PDF / video / office thumbnails        |
| `s3`     | SeaweedFS + bucket init  | Developing against the S3 blob backend |
| `idp`    | Logto + its own Postgres | Working without a Logto Cloud tenant   |

```bash
docker compose -f deploy/compose/compose.dev.yml --profile worker up -d
```

The worker runs in Docker even in development, because the renderers need
`pdftoppm` and `ffmpeg`. Building it on a machine without poppler is worse than
useless: it registers no `render:pdf`, so PDFs silently never thumbnail. Its
blob directory is bind-mounted to `apps/server/data/blobs` — the same store
your host-run server writes to. If they diverge, thumbnails are generated
somewhere the server cannot read and every card stays on `preview not ready`.

## Operational notes

**Startup order.** `cmd/server` owns migrations and applies them before it
waits on OIDC. `cmd/worker` never runs DDL — it calls `store.AwaitSchema` and
polls. Two processes racing DDL on a cold start can both apply the same file.

**The server blocks on OIDC discovery** for up to two minutes at startup, then
exits and is restarted. `web` therefore depends on `server` for _start order
only_, never `condition: service_healthy` — otherwise an IdP outage takes the
whole site down, static bundle included. nginx resolves the API through
Docker's DNS per-request, so it stays up and returns 502 until the API is back.

**Storage.** `PP_BLOB_BACKEND=local` keeps blobs in a volume, which pins the
stack to one host. `s3` is what allows more than one `server` replica. The key
layout is identical, so migrating is a plain copy:
`aws s3 sync ./data/blobs s3://<bucket>/`. See
[storage.md](architecture/storage.md).

Object storage can be external (AWS, Wasabi, Backblaze) or the bundled
**SeaweedFS** under `--profile s3`, in both compose files. SeaweedFS passes the
same `runStoreConformance` suite as local disk and MinIO, with no code changes
— that is what `blob.Store` being an interface buys. In production its
credentials come from `SEAWEEDFS_ACCESS_KEY`/`SEAWEEDFS_SECRET_KEY` and the
config is written at startup, so no secret ships in the repo; the committed
`seaweedfs/s3.json` holds development values only.

Two SeaweedFS details worth knowing: `weed` binds its master port to the
container IP rather than loopback, so healthchecks must target `$(hostname -i)`
and not `localhost`; and the app never creates buckets, so `seaweedfs-init`
does it once.

**OnlyOffice JWT must stay disabled.** Neither the WOPI integration nor the
preview worker's conversion calls sign their requests. `JWT_ENABLED=true`
breaks both; office thumbnails fail with conversion error `-8`.

**Resources.** Document Server wants 2–4 GB RAM and is by far the heaviest
service. The worker is bursty — it spawns `pdftoppm`/`ffmpeg` per job with a
30s timeout each.

## Known gaps

- **The session cookie does not set `Secure`** (`internal/auth/auth.go`). Over
  HTTPS it still works, but the browser will also send it over plain HTTP, so
  it is exposed to interception on any accidental downgrade. The fix is one
  line — set `Secure` when `PP_EXTERNAL_URL` is `https://` — and should land
  before this is exposed to the internet.
- **No JWT signing** for OnlyOffice, as above.
- **`onlyoffice/documentserver:latest`** is unpinned; pin a version before
  relying on it, or an upstream release can change the editor under you.
- **No backup job.** `pg_data` and `blob_data` both need one; a database dump
  without the blobs restores metadata pointing at content that is gone.
