# Deployment

Two compose files, three env files, one convention.

| Path                               | Committed?  | What it is                                     |
| ---------------------------------- | ----------- | ---------------------------------------------- |
| `apps/server/.env.example`         | yes         | Template for the host-run Go server            |
| `apps/server/.env`                 | no, ignored | Your local values — **source it**              |
| `deploy/compose/.env.dev.example`  | yes         | Template for `compose.dev.yml` overrides       |
| `deploy/compose/.env`              | no, ignored | Dev overrides — compose auto-reads it          |
| `deploy/compose/.env.prod.example` | yes         | Template for production                        |
| `deploy/compose/.env.prod`         | no, ignored | Production values — pass with **`--env-file`** |

`*.example` files are committed; the copies you fill in are not.

**Production must be started with `--env-file deploy/compose/.env.prod`.** Both
compose files live in the same directory, and compose auto-loads `.env` from
there — which is the _development_ file. Without the flag a production stack
silently inherits `PP_DEV_INSECURE=1` and development S3 credentials. Passing
`--env-file` replaces that default lookup entirely, which is why the dev and
prod files are named differently instead of both being `.env`.

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

- the session cookie is host-only (no `Domain`), `HttpOnly`, `SameSite=Lax`
  and `Secure` whenever `PP_EXTERNAL_URL` is `https://`
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
cp deploy/compose/.env.prod.example deploy/compose/.env.prod   # then fill it in
docker compose -f deploy/compose/compose.prod.yml \
  --env-file deploy/compose/.env.prod up -d --build
```

Required values have no defaults — compose refuses to start and names the
missing variable rather than booting a half-configured stack.

Generate the master secret once and keep it:

```bash
openssl rand -base64 32
```

`PP_SESSION_KEY` no longer signs sessions — those are rows in the `sessions`
table, so **rotating it does not sign anybody out**. The variable keeps its
historical name deliberately: it is written into every existing deployment's
env file, and renaming it would fall back to an ephemeral key silently and
break office previews at runtime rather than at startup. What it is now is the
master secret every signer derives a purpose-bound subkey from
(`config.DeriveKey`, HKDF-SHA256). Today the only consumer is the WOPI access
token, so rotating it invalidates the tokens currently in flight — an open
editor session and any conversion job mid-retry — and nothing else.

It still has to be **set, and identical in the server and the worker**. The
worker signs office-conversion tokens the _server_ verifies, so a mismatch
means office thumbnails silently never appear; left unset, each process
generates its own key at startup and the worker declines to register the
office renderer rather than failing every job with an opaque token error.

`PP_EXTERNAL_URL` must be `https://` in production. The `Secure` flag on every
cookie the server sets is derived from its scheme — there is no separate
switch — so an `http://` value ships session cookies a browser will happily
send over plain HTTP, and one accidental downgrade exposes a live session to
anyone on the path. The server says so at startup:
`PP_EXTERNAL_URL is not https: session cookies will be sent without the Secure flag`.
If that line is in your production logs, the deployment is misconfigured; it is
also the OIDC redirect base, so the scheme has to match what you registered
with the provider anyway.

Register with your OIDC provider:

- redirect URI: `https://drive.example.com/auth/callback`
- post-logout redirect: `https://drive.example.com`

### One issuer or two

`PP_OIDC_ISSUER` is the issuer **this process** can reach: discovery, token,
userinfo and JWKS are all fetched from it. `PP_OIDC_ISSUER_EXTERNAL` is the
issuer **the browser** is sent to, and the value that must appear in the `iss`
claim. In the ordinary deployment the IdP has one address, the external one is
left unset and defaults to the first, and none of this does anything.

They differ only in a split horizon — the IdP answering on a container
hostname inside the network and a public domain outside it:

```
PP_OIDC_ISSUER=http://logto:3001/oidc            # server → IdP, container network
PP_OIDC_ISSUER_EXTERNAL=https://id.example.com/oidc   # browser → IdP, and the `iss` claim
```

Only the two endpoints the browser visits are rebased onto the external
origin: **authorization** and **end-session**. Token, userinfo and JWKS stay on
the internal one, because only the server calls them. Get this backwards and
the symptom is either a browser redirected to a hostname it cannot resolve, or
an ID token rejected because its `iss` does not match what was expected.

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
docker compose -f deploy/compose/compose.prod.yml \
  --env-file deploy/compose/.env.prod run --rm worker --backfill
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

`PP_EXTERNAL_URL` is the origin the _browser_ uses, which in development is the
Vite dev server (`http://localhost:5173`): it proxies `/api`, `/auth` and `/s/`
to `:8080`, so the redirect to `/` after an OIDC callback lands on the SPA. The
IdP's redirect URI must be `http://localhost:5173/auth/callback` to match.

If port 5432 is already taken on your host, set `PP_POSTGRES_PORT` in
`deploy/compose/.env` and use the same port in `PP_DATABASE_URL`.

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
useless: it registers no `render:pdf`, so PDFs silently never thumbnail.

It reads and writes the same blobs as the server, so both must use the same
backend. On `local` its blob directory is bind-mounted to
`apps/server/data/blobs` — the store your host-run server writes to; if they
diverge, thumbnails land somewhere the server cannot read and every card stays
on `preview not ready`. On `s3` set `PP_BLOB_BACKEND=s3` in both
`apps/server/.env` and `deploy/compose/.env`; only the endpoint differs, the
host server using `http://localhost:8333` and the worker
`PP_S3_ENDPOINT_INTERNAL=http://seaweedfs:8333`. Switching backends does not
move existing blobs.

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
— that is what `blob.Store` being an interface buys. In production it reads the
same `PP_S3_ACCESS_KEY_ID`/`PP_S3_SECRET_ACCESS_KEY` the server and worker use
and writes its own config at startup, so no secret ships in the repo; the
committed `seaweedfs/s3.json` holds development values only. There is
deliberately no second `SEAWEEDFS_*` credential pair — two variables that must
match are two variables that eventually will not, and requiring them also made
the default local-disk stack refuse to start over a service it never runs.

Two SeaweedFS details worth knowing: `weed` binds its master port to the
container IP rather than loopback, so healthchecks must target `$(hostname -i)`
and not `localhost`; and the app never creates buckets, so `seaweedfs-init`
does it once.

**OnlyOffice JWT must stay disabled.** Neither the WOPI integration nor the
preview worker's conversion calls sign their requests. `JWT_ENABLED=true`
breaks both; office thumbnails fail with conversion error `-8`.

**`ALLOW_META_IP_ADDRESS` / `ALLOW_PRIVATE_IP_ADDRESS` must stay enabled** in
both compose files. OnlyOffice's `request-filtering-agent` refuses to fetch
from anything it classifies as a private/link-local/meta address by default.
That covers `host.docker.internal` in development (OrbStack resolves it to
`0.250.250.254`) _and_ `http://server:8080` in production, whose container IP
sits in `172.16.0.0/12`. Without these, every conversion fails with the
unhelpful `{"error":-4}`; the real reason
(`DNS lookup … is not allowed. Because, It is meta IP address.`) appears only
in `docker logs <onlyoffice container>`, never in the API or the job's
`last_error`.

**The web image compresses and caches deliberately.** nginx ships `#gzip on;`
commented out, so `nginx.conf` enables it — the entry chunk is 378 KB raw
against 116 KB gzipped. Hashed assets get
`Cache-Control: public, max-age=31536000, immutable` and `index.html` gets
`no-cache`, so a deploy is visible immediately without ever re-fetching an
unchanged chunk.

**Images.** Three, from two Dockerfiles:

| Image    | Dockerfile                      | Base                | Size   |
| -------- | ------------------------------- | ------------------- | ------ |
| `server` | `apps/server/Dockerfile:server` | `alpine:3.21`       | 35 MB  |
| `worker` | `apps/server/Dockerfile:worker` | `alpine:3.21`       | 207 MB |
| `web`    | `apps/web/Dockerfile`           | `nginx:1.27-alpine` | 80 MB  |

The Go compile stages are split per binary, so `--target server` never links
the worker. Both share one `deps` stage and use BuildKit cache mounts for the
module and build caches, so a source-only change rebuilds in seconds. Binaries
are built `-trimpath -ldflags="-s -w"`; the symbol tables are about a third of
the unstripped size and nothing in production reads them.

`alpine` is kept as the runtime base rather than `scratch` because it already
carries `ca-certificates-bundle`, which the OIDC and S3 clients need for
outbound TLS. The worker image is the large one purely because of
`poppler-utils` and `ffmpeg` — the reason those live there and not next to the
API is containment, not size.

`apps/web/Dockerfile` builds from the **repo root**, because the app imports
`@workspace/ui` and the whole bun workspace has to be present. `.dockerignore`
excludes the agent scratch directories (`.claude`, `.agents`, `.plans`, …) for
cache reasons more than size: they change constantly, and `COPY . .` would
otherwise invalidate the Vite build layer on every change.

**Resources.** Document Server wants 2–4 GB RAM and is by far the heaviest
service. The worker is bursty — it spawns `pdftoppm`/`ffmpeg` per job with a
30s timeout each.

## Known gaps

- **No CSRF defence beyond `SameSite=Lax`** — no `Origin` or `Referer` check
  on the API. `Lax` covers top-level navigations and the API is JSON over
  `fetch`, so this is thin rather than absent. See
  [production-readiness.md](architecture/production-readiness.md) §3, which
  also records the cookie and session weaknesses that M20 closed.
- **No JWT signing** for OnlyOffice, as above.
- **No backup job.** `pg_data` and `blob_data` both need one; a database dump
  without the blobs restores metadata pointing at content that is gone.
