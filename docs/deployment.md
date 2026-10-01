# Deployment

Three compose files, three env files, one convention. For a step-by-step
install, read [self-hosting.md](self-hosting.md) first; this page is the
reference behind it.

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

Three compose files, for three different jobs:

| File                               | What it runs                                     | Use it when                                 |
| ---------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| `deploy/compose/compose.prod.yml`  | pp-hub, from the published images                | Deploying to a server                       |
| `deploy/compose/compose.logto.yml` | Self-hosted Logto and its own Postgres           | Running Logto next to pp-hub                |
| `deploy/compose/compose.dev.yml`   | Only the things the app talks to — no app images | Developing, running Go and Vite from source |

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

It is also a rule the server enforces, not only a consequence of the cookie:
every mutation under `/api/v1`, plus `POST /auth/logout`, is refused unless
its `Origin` (or the origin of its `Referer`) equals `PP_EXTERNAL_URL`. So
that variable must be the address users actually type, scheme included — a
stale value there does not degrade gracefully, it refuses every write with
403 `cross-origin request refused`. `/wopi/*` is exempt by design, because the
Document Server sends no `Origin` and presents a signed token instead.

The Go server serves **only** `/healthz`, `/auth/*`, `/api/v1/*`, `/hooks/*`,
`/wopi/*` and `/s/*` — it has no static file handler. So something has to serve the bundle
and proxy the API on one hostname. That is what the `web` container is:
nginx with the built SPA and a proxy to `server:8080`
(`apps/web/Dockerfile`, `apps/web/nginx.conf`).

`/wopi/*` is the one group nginx does **not** proxy: the Document Server
reaches it over the compose network, so publishing it would put a bearer token
on the public internet for nothing. See
[`/wopi/` is not published](#wopi-is-not-published).

```
                    ┌──────────────────────────────────────┐
 drive.example.com ─┤ web (nginx)                   [edge] │
                    │   /              → SPA bundle        │
                    │   /api /auth /s  → server:8080       │
                    │   /hooks         → server:8080       │
                    │   /wopi          → not proxied       │
                    └───────────────────┬──────────────────┘
                                        │ edge
                          ┌─────────────▼─────────────┐   ┌──────────────────┐
                          │ server  [edge data office]├───┤ postgres  [data] │
                          └─────────────┬─────────────┘   └────────┬─────────┘
                                        │ office                   │ data
                          ┌─────────────▼─────────────┐            │
                          │ worker (optional)         ├────────────┘
                          │            [data office]  │
                          └─────────────┬─────────────┘
                                        │ conversion (office)
 office.example.com ──────────► onlyoffice                    [office]
                                        │ WOPI → server:8080  (office)
```

The bracketed names are compose networks, not decoration: `onlyoffice` is on
`office` only, so it can reach `server:8080` and nothing else. See [Network
segments](#network-segments).

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
  --env-file deploy/compose/.env.prod up -d
```

Required values have no defaults — compose refuses to start and names the
missing variable rather than booting a half-configured stack.

### Releases

`up -d`, not `up -d --build`. The three application images are published by
`.github/workflows/release.yml` to
`ghcr.io/pp-techs/pp-hub/{web,server,worker}`, and `PP_IMAGE_TAG` names the
one this stack runs. Pushing the git tag `v0.1.0` publishes `:0.1.0`, `:0.1`,
`:latest` and `:sha-<short>` — the image tag drops the `v`; a push to `main`
publishes `:edge` and `:sha-<short>`. Prerelease tags (`v2.0.0-beta.1`) get
their own tag but do not move `:latest`.

| Action        | What you do                                      |
| ------------- | ------------------------------------------------ |
| Deploy        | set `PP_IMAGE_TAG=0.2.0`, re-run `up -d`         |
| Roll back     | set it back to `0.1.0`, re-run `up -d`           |
| Build locally | add `-f compose.build.yml`, then `up -d --build` |

`PP_IMAGE_TAG` has no default: pin a released version. `:latest` is published
on every release for consumers who want the moving target, but `latest` is
not a version — it cannot be rolled back to, two hosts pulling it can run
different code, and nothing on the box can then say what is deployed. The
variable being required, and pinned to a version, is what makes "which commit
is in production" a question with an answer.

Building on the deployment host is the thing this replaces. It puts a bun
install, a Vite build and two Go builds on a box sized for serving, makes
every deploy's output depend on the machine that ran it, and turns a rollback
into a rebuild — minutes of downtime to return to code that was already
working. `compose.prod.yml` therefore has no `build:` at all; building a tag
that does not exist yet is the `compose.build.yml` override, which only
resolves from a full checkout:

```bash
docker compose -f deploy/compose/compose.prod.yml \
  -f deploy/compose/compose.build.yml \
  --env-file deploy/compose/.env.prod up -d --build
```

The web image is deployment-agnostic, which is what makes one tag correct
everywhere: the SPA reads nothing from `import.meta.env`, and the single
environment-specific value in `nginx.conf` (`PP_ONLYOFFICE_URL`, named in the
CSP) is substituted by envsubst when the container starts. There is no
per-environment build.

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

Register with Logto, on the sign-in application behind `PP_OIDC_CLIENT_ID`:

- redirect URI: `https://drive.example.com/auth/callback`
- post-logout redirect: `https://drive.example.com`

That is the login half. Logto is the only identity provider pp-hub supports,
and the server refuses to start without the rest of the same tenant — an M2M
application, an organization template, an email connector, a webhook and two
consent scopes — all of it in
[Logto: identity and membership](#logto-identity-and-membership).

### The security and limit variables

Four variables added with the M21 security pass. None is required; each has a
default, and each has a way of being wrong that is worth understanding before
it bites.

**`PP_BOOTSTRAP_ADMIN_EMAIL`** — strongly recommended on a fresh install, and
empty by default. Whoever signs in with this address becomes an instance
admin, **on every sign-in**. `users.is_admin` defaults to false and its only
writer is an admin-only endpoint, so without this the first administrator can
only be made with `UPDATE users SET is_admin = true` in psql. Two
consequences:

- It requires a **verified** email claim. The IdP must assert
  `email_verified` for the address, or the promotion is refused with
  `refusing admin promotion for an unverified email` in the log and a
  `login_failed` row in the security log. This is not pedantry: the variable is
  a standing authorization rule keyed on a claim, and an IdP that lets users
  edit their own address would otherwise let any of them take the instance.
- Because it re-promotes on every sign-in, **that account cannot be demoted
  while the variable is set** — the demotion sticks until their next login.
  That is deliberate (it is also the recovery path for an install whose last
  admin was demoted by accident), but unset the variable once a second admin
  exists, or the console will keep undoing you.

**`PP_MAX_UPLOAD_BYTES`** — default `5G`; accepts plain bytes or a `K`/`M`/`G`/`T`
suffix; `0` disables the ceiling. This is the only thing bounding a single
upload. Since M30 an upload is a tus session, and the ceiling is checked
against the `Upload-Length` it declares when it is created — a file that is
too big is refused with 413 before a byte is sent — and then enforced on every
chunk, which is never allowed to write past that declared length. A space's
`quota_bytes` is checked at creation too, and again _after_ the bytes have
landed, since only the measured size is trustworthy; it defaults to `0`,
meaning unlimited, so with no ceiling one authenticated caller can fill the
volume. Set too low, large legitimate uploads fail with 413 and a clear
message; set to `0`, you are back to the original problem. A value that will
not parse is a **startup error**, not a silent default: an operator who typed
`5GB` meaning a ceiling should not be handed "no ceiling" for the trouble.
This serves as the initial deployment default and can also be adjusted dynamically
at runtime without container restarts by an administrator in **Admin > Settings** (`/admin/settings`).
Note that `client_max_body_size 0` in `nginx.conf` is intentional — the edge
streams the body through and the app answers 413 with a sentence, instead of
nginx rejecting it with a bare page long before the app can say why.

**`PP_BLOB_SCRUB_BYTES_PER_DAY`** — default `100G`; same syntax as
`PP_MAX_UPLOAD_BYTES`; `0` disables it. How much stored content the hourly
scrub re-hashes per day, least recently verified first — 100G cycles 5 TB in
about 50 days. Nothing else ever reads a blob nobody opens, so without it
disk rot or a torn write surfaces only when somebody needs the file. Damage
is logged, marked on the blob and exported as `pphub_blobs_damaged`; alert on
it being non-zero. On an S3 backend every scrubbed byte is a download, so size
the budget against the provider's egress terms. See
[storage.md](architecture/storage.md#scrub).

**`PP_AUDIT_RETENTION_DAYS`** — default `365`. How long `security_events` rows
live; the five-minute sweep in `cmd/server` purges anything older. One row is
written on every sign-in and every failed attempt, so setting it very high on
a busy instance grows a table nobody can page through, and setting it very low
throws away the history an incident needs — a compromise noticed in March
wants the failed sign-ins from February. Unparseable values fall back to the
default and say so rather than refusing to start, because a retention window
is a policy knob. Can also be overridden at runtime via **Admin > Settings** (`/admin/settings`).

Four related knobs have no environment variable and live only in
**Admin > Settings**: `read_audit_retention_days` (default `90`) — how long
file access events (downloads, views, editor opens) are kept, separately from
the rest of the log because they are most of it —
`alert_failed_login_threshold` (`10`, `0` turns alerts off) with
`alert_failed_login_window_minutes` (`15`), which decide when a run of failed
sign-ins from one address raises a security alert, emails the instance
admins and increments `pphub_security_alerts_total{rule}` on the metrics
listener, and `version_retention_count` (`100`, `0` keeps every version,
at most `10000`) — how many versions of each file are kept, current
included. Older ones are deleted when a file gets a new version, and GC then
reclaims their bytes; lowering it applies to each file on its next save, not
retroactively. Every stored version counts against a space's quota, so this
is the knob that keeps edit-heavy documents from filling it.

**`PP_LOG_LEVEL`** / **`PP_LOG_FORMAT`** — defaults `info` / `text` in the
binary; `compose.prod.yml` sets `json`. Both the server and the worker read
them. Every record logged while serving a request carries `request_id`, the
same value returned to the caller as `X-Request-Id` and stored on the
`security_events` rows that request wrote — ask a user for that header and grep
for it. The access log is one `"http request"` record per request (redacted
path, chi route pattern, status, duration, client address, `user_id` once
signed in); `/healthz` and `/readyz` log at `debug` so the compose probes do
not flood `info`. An unknown value warns and falls back to the default.

**`PP_METRICS_ADDR`** — default `:9091`; `off` disables it. The server's
Prometheus endpoint, `GET /metrics`, on its own listener so a proxy mistake
cannot make it public. `compose.prod.yml` only `expose`s it on the internal
networks; to scrape it, attach your Prometheus container to the `edge` network
and target `server:9091`. Series are prefixed `pphub_`: HTTP requests by
method/route/status and latency, the Postgres pool, `pphub_jobs{kind,status}`
with the oldest job's age (a growing `render:*` backlog means no worker),
referenced vs. unreferenced (awaiting GC) blobs and bytes,
`pphub_blobs_damaged` (blobs the scrub found missing or not matching their
hash — alert on > 0), `pphub_blob_disk_free_bytes` and
`pphub_blob_disk_size_bytes` for the filesystem holding `PP_BLOB_ROOT` (alert
before it fills: uploads are what fail first), the block editor's
collaboration rooms — `pphub_collab_rooms` (resident, idle ones included),
`pphub_collab_peers`, `pphub_collab_flush_duration_seconds` and
`pphub_collab_flush_failures_total` (write-backs that failed and are held in
memory for a retry; alert on a rising count) — and Go/process metrics. The
worker has no metrics port.

**`PP_TRUSTED_PROXIES`** — a comma-separated list of CIDRs (a bare address is
accepted as its own `/32`), empty by default, `172.16.0.0/12` in the compose
file. This is the one where **both** a missing value and an over-broad value
are harmful, in opposite ways:

- **Unset behind a proxy**: no forwarded header is believed, so every request
  appears to come from the proxy's address. The per-IP rate limits then share
  one bucket across the whole internet — one caller can spend the instance's
  entire budget and lock everybody else out of sign-in — and every session row
  and security event records the proxy's address instead of the user's, which
  makes the audit log useless for the question it exists to answer.
- **Set too broadly** (`0.0.0.0/0`, or a range wider than your actual hops):
  the server believes `X-Forwarded-For` from anybody, which hands the caller
  their own identity back. Rate-limit buckets become resettable at will by
  appending a header, and the address shown on somebody's Sessions tab becomes
  whatever an attacker chose. That is precisely the behaviour chi's
  deprecated `RealIP` had, and the reason it is gone.

Name the hops you actually have and nothing more. On the compose stack that is
the `web` container's network; add Traefik's or your own proxy's range if it
adds another hop. Empty is **correct** for a server reached directly, and
means "believe the TCP peer". The list is validated at startup, so a typo is a
sentence (`PP_TRUSTED_PROXIES: "10.0.0" is neither an address nor a CIDR`)
rather than a panic out of chi.

The rate limits themselves are not configurable, and are worth knowing:
`/auth/login`, `/auth/callback`, `/auth/logout` at 30/min with a burst of 10;
`/s/{token}` at 60/min burst 20; `/api/v1` at 1200/min burst 200. An exhausted
bucket answers 429 with `Retry-After`. **The buckets live in the process**, so
`N` `server` replicas allow `N` times those rates — scaling out scales the
limit with you. That is a deliberate trade against making Redis a dependency
of a deployment that otherwise does not need it, and it is stated here rather
than hidden because an operator running replicas should know.

### Logto: identity and membership

Space membership is a projection of a Logto organization, so Logto is not only
the login provider — it is where "who works here" is decided, where the
companies themselves are created, renamed and linked, and where the invitation
email comes from. It is also mandatory: there is no other provider and no mode
without it. Five things must exist in the Logto tenant before any of it works,
and each one fails in its own way if it is missing. See
[logto-org-sync.md](architecture/logto-org-sync.md) for the design; this is
the checklist. **`deploy/logto/pp-logto-setup.sh` creates all five** (plus
the sign-in application, the SMTP connector and email sign-up, and — for
[agent access](#agent-access-mcp) — the `pp-hub MCP` API resource and CIMD)
through the Management API from one bootstrap machine-to-machine app,
idempotently, and writes the resulting values into an env file — see
[self-hosting.md](self-hosting.md#3-configure-logto-for-pp-hub).

1. **A machine-to-machine application**, granted the **Logto Management API**
   role that carries the `all` permission. This is what reads an
   organization's name and member list and writes organizations, membership,
   roles, suspensions and invitations back. Its id and secret are
   `PP_LOGTO_M2M_CLIENT_ID` / `PP_LOGTO_M2M_CLIENT_SECRET` — **required**: the
   server refuses to start without them — and pp-hub exchanges them for a token
   with `client_credentials` against `PP_LOGTO_API_RESOURCE`. Without the `all`
   permission the exchange succeeds and every call answers 403, so the symptom
   is membership that never converges rather than a startup error.
2. **An organization template with the roles `admin` and `member`.** The
   template is tenant-wide — every organization shares one role set — and the
   `admin` role is the one pp-hub maps onto a space's automatic `Admin` group.
   Rename it and set `PP_LOGTO_ORG_ROLE_ADMIN` to match, or nobody is an admin
   anywhere.
3. **An email connector, with the `OrganizationInvitation` template filled
   in.** Adding a member by an address that has no account upstream creates a
   Logto organization invitation, and Logto sends it. pp-hub mints no token and
   has no fallback to copy and paste: without the connector, the invitation
   call fails and `POST /spaces/{id}/members` answers **502** with Logto's
   refusal for an address that has no upstream account. The template **must render `{{link}}`** —
   pp-hub fills it with `{PP_EXTERNAL_URL}/auth/login?login_hint=<address>`,
   and the invitation is accepted when the invitee first signs in with that
   address verified. Email sign-up must therefore be enabled in the sign-in
   experience, with email verification on, or the invitee has no way to
   create the account the invitation is waiting for.
4. **A webhook subscribed to `Organization.Membership.Updated`,
   `Organization.Data.Updated`, `Organization.Deleted`, `User.Data.Updated`,
   `User.SuspensionStatus.Updated` and `User.Deleted`**, pointing at
   `POST {PP_EXTERNAL_URL}/hooks/logto`. Copy its signing key into
   `PP_LOGTO_WEBHOOK_KEY`; the handler verifies an HMAC-SHA256 of the raw body
   against `logto-signature-sha-256` and drops anything that does not match —
   including every delivery, if the key is unset.
   The endpoint is deliberately **outside `/api/v1`** — that group is
   authenticated and same-origin-checked by a router test, and an
   unauthenticated endpoint inside it would mean loosening the test that
   protects the whole API. A lost delivery is not a correctness problem: the
   hourly reconcile and the next sign-in both converge the same way.
   **Logto refuses to deliver to private or special-use addresses** (its
   delivery log reads `hostname resolves to a special-use IP address`), so the
   URL must resolve to a public IP from inside the Logto container. A
   deployment whose pp-hub domain resolves to a LAN address, and every local
   development setup, runs without webhooks: changes made in Logto then arrive
   at the next sign-in or on the hourly sweep.
5. **Two user-consent scopes on the sign-in application** (the one behind
   `PP_OIDC_CLIENT_ID`): `urn:logto:scope:organizations` and
   `urn:logto:scope:organization_roles`. pp-hub always requests them; they put
   `organizations` and `organization_roles` in the ID token, which is how a
   returning user's membership is corrected at sign-in. Omit them and sign-in
   still works, but the token carries no `organizations` claim: the server
   logs `ID token carries no organizations claim` and skips the sign-in
   reconcile — the path that heals a lost webhook — rather than reading the
   absence as "left every organization".

`PP_LOGTO_ENDPOINT` and both credentials are required, and the endpoint must be
an absolute URL; anything less is a startup error naming the missing variable,
because every membership, space and suspension write goes through the
Management API and there is no degraded mode to fall back to. The worker never
talks to Logto and does not need them. `PP_LOGTO_WEBHOOK_KEY` alone may be
missing: the server warns at startup that `/hooks/logto` refuses every
delivery, and changes made in Logto are then picked up only at the next
sign-in or on the hourly sweep.

| Variable                     | Required             | What it is                                                                                                                                               |
| ---------------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PP_LOGTO_ENDPOINT`          | **yes** (server)     | The Logto **base** URL — `https://<tenant>.logto.app`, not the OIDC issuer, and **no `/oidc` suffix**. Must be absolute                                  |
| `PP_LOGTO_M2M_CLIENT_ID`     | **yes** (server)     | The M2M application's id                                                                                                                                 |
| `PP_LOGTO_M2M_CLIENT_SECRET` | **yes** (server)     | Its secret                                                                                                                                               |
| `PP_LOGTO_API_RESOURCE`      | no — default below   | Management API resource indicator: `https://<tenant-id>.logto.app/api` on Cloud, `https://default.logto.app/api` (the default) on OSS                    |
| `PP_LOGTO_WEBHOOK_KEY`       | no                   | The webhook's signing key. Unset, every delivery is refused and a warning says so; membership then converges at the next sign-in and on the hourly sweep |
| `PP_LOGTO_ORG_ROLE_ADMIN`    | no — default `admin` | Which organization role means admin                                                                                                                      |

Two of these are easy to get subtly wrong, and both fail at the first API call
rather than at startup:

- **`PP_LOGTO_ENDPOINT` is not `PP_OIDC_ISSUER`.** The issuer ends in `/oidc`;
  this one must not. They point at the same host in the ordinary deployment
  and are still different values.
- **`PP_LOGTO_API_RESOURCE` is an identifier, not an address pp-hub fetches.**
  On Cloud it contains the **tenant id**, which is not the vanity subdomain in
  your console URL; on self-hosted OSS it is the literal
  `https://default.logto.app/api` regardless of where Logto actually runs.
  A wrong value produces a token the Management API rejects.

A space and an organization are linked by `spaces.idp_org_id`. Every space
created in pp-hub creates its organization first, and an organization's first
member to sign in brings its space into existence, so on a fresh install the
link is never made by hand. A space that mirrors nothing — one created before
the sync, or one whose organization was deleted in Logto, which unlinks rather
than empties it — shows an alert on its People pane, and membership writes
there answer **422**. An instance admin fixes it with **Link to Logto** on that
alert: it previews which members can be pushed into a new organization and
which have no Logto account and will lose access, creates the organization,
and pushes the members in a background job. Deleting a space keeps its
organization in Logto; the next sign-in does not recreate the space.

### Response headers and the CSP

`nginx.conf` sends, on every response and with `always` so they survive the
502s the config is designed to return while the API restarts:

| Header                      | Value                                 |
| --------------------------- | ------------------------------------- |
| `Content-Security-Policy`   | see below                             |
| `X-Frame-Options`           | `DENY`                                |
| `Referrer-Policy`           | `strict-origin-when-cross-origin`     |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` |
| `X-Content-Type-Options`    | `nosniff`                             |

plus `server_tokens off`, so no version number appears on responses or error
pages. HSTS assumes you terminate TLS in front of this container, which the
`https://` requirement on `PP_EXTERNAL_URL` already assumes.

The policy itself:

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline';
img-src 'self' data: blob:; media-src 'self' blob:; object-src 'self';
frame-src 'self' ${PP_ONLYOFFICE_URL}; form-action 'self' ${PP_ONLYOFFICE_URL};
frame-ancestors 'none'; base-uri 'none';
connect-src 'self' wss://$http_host ws://$http_host
```

`connect-src` names the block editor's collaboration socket explicitly. CSP3
lets `'self'` cover a `wss:` URL on the same host, but older WebKit does not
match a `ws(s)` URL against `'self'`. `$http_host` is nginx's own variable —
the host the browser asked for, so always this deployment — and survives the
`PP_` envsubst filter; an https page cannot open a `ws:` socket anyway (mixed
content), so that entry costs it nothing.

Two directives are **deliberately looser than every guide recommends, and
tightening them breaks the app**:

- **`object-src 'self'`, not `'none'`.** The PDF viewer is an `<object>`
  pointed at `/api/v1/nodes/{id}/content`. With `'none'` the browser blocks it
  and renders the fallback children instead — so it fails as a silent UI
  regression, not an error anybody sees in a log.
- **`form-action` and `frame-src` name `${PP_ONLYOFFICE_URL}`.** The editor
  handoff is a real cross-origin form POST into an iframe: 9.x will not accept
  the access token as a query parameter on that route, so the browser submits
  a form to the Document Server's origin. With `'self'` alone the editor frame
  is simply blank.

One more is looser than it should be, for a reason worth fixing later:
**`style-src 'unsafe-inline'`** covers a handful of React `style` props
(progress meters, tree indentation) plus sonner's runtime `<style>` injection.
It is the only directive here that needs changes in `apps/web` to tighten,
rather than a configuration decision.

`script-src` needs no nonce: the built `index.html` carries zero inline
script and the SPA uses `dangerouslySetInnerHTML` nowhere.
`frame-ancestors 'none'` is safe because pp-hub is always the framer and never
the framed — nothing reads `window.parent` or listens for `postMessage`.

**The template mechanics.** `nginx.conf` is not a finished config: the CSP has
to name the Document Server's origin, and hardcoding it would break the first
deployment that moves the editor. The Dockerfile copies it to
`/etc/nginx/templates/default.conf.template`, which the official nginx image
runs `envsubst` over at container start, and sets
`NGINX_ENVSUBST_FILTER=PP_` so substitution is restricted to `PP_*` — without
that filter, nginx's own `$host`, `$remote_addr`, `$uri` and friends would be
eaten and the config would not even load. `compose.prod.yml` therefore passes
`PP_ONLYOFFICE_URL` to the `web` service as well as to `server`; if you run
the image by hand, it must be in that container's environment or the CSP ships
with an empty `frame-src` entry and the editor frame goes blank.

The headers are **repeated** inside `location /assets/` and
`location = /index.html`. That is not an accident and must not be
"simplified": nginx does not merge `add_header` across levels — a `location`
that declares any `add_header` of its own discards every inherited one, and
both of those locations declare a `Cache-Control`. Without the repetition the
bundle and the HTML entry point, the two responses a CSP is actually for,
would ship bare. If you add a `location` with its own `add_header`, copy the
block.

The access log is a custom `log_format` with a `map` that rewrites
`/s/<token>` to `/s/REDACTED` before the line is written, because a share
token is the entire authorization for anonymous access to a file and only its
SHA-256 is stored in the database. The Go side does the same for its own log
and for the WOPI `access_token` query parameter.

### Block document collaboration

The block editor (M39, [block-documents.md](architecture/block-documents.md))
keeps a WebSocket open to `GET /api/v1/collab/{room}`, and nginx gives it a
location of its own: `location /api/v1/collab/` passes `Upgrade` and
`Connection` (a `map` turns an empty `Upgrade` into `close`), sets a 1 h
`proxy_read_timeout`/`proxy_send_timeout` instead of the upload location's ten
minutes, and turns buffering off. The server pings every socket every 15 s, so
an hour of silence is a dead peer. Because nginx inherits `proxy_set_header`
only into a location that sets none of its own, that location repeats every
server-level header, the cleared client-address headers included — keep them
in step if you change one. A proxy in front of nginx must pass WebSocket
upgrades on this path too.

The socket is refused (403) unless its `Origin` is `PP_EXTERNAL_URL`'s
origin exactly, and a missing `Origin` is refused as well.

**One `server` instance hosts every room.** A room's live document, its epoch
and the file lock it holds belong to the process; at startup the server clears
every `collab:` lock a previous process left, which is only safe because no
other instance can be holding one — and it does so only once its listener is
bound, so a second copy started by mistake fails on the port before it can
strip the running one's rooms of their locks. Several instances would need
ygo's cluster relay and are out of scope, like the rate limiter. On `SIGTERM`
the server stops accepting, then writes every room back before exiting, so up
to ~5 min of edits are at risk only on a crash, not a deploy.

### Notification stream

In-app notifications (M29) arrive over Server-Sent Events at
`GET /api/v1/notifications/stream`, and nginx gives it an exact-match
location: `location = /api/v1/notifications/stream` with `proxy_buffering off`
and `proxy_cache off` — buffered, each event would sit in nginx until the
buffer filled, i.e. forever — and a 1 h read/send timeout. The server writes a
heartbeat comment every 25 s, so an hour of silence is a dead client. The
location sets no `proxy_set_header`, so the server-level headers are
inherited. A proxy or CDN in front of nginx must not buffer this path either;
the client reconnects with backoff and refetches the list, so a proxy that
cuts idle responses costs only a reconnect.

Like the rooms, the streams belong to one process
(`notifications.Broker`), and so does the `notify:mentions` job that feeds
them: it runs in `server`, never in `worker`. One `server` instance.

### Agent access (MCP)

AI agents (M41, [agent-access.md](architecture/agent-access.md)) connect to
`${PP_EXTERNAL_URL}/mcp/` — Streamable HTTP, stateless, JSON responses — and
find their authorization server through RFC 9728 metadata at
`/.well-known/oauth-protected-resource` (and `…/mcp`), which names the
resource and Logto's external issuer. An unauthenticated call gets 401 with
`WWW-Authenticate: Bearer resource_metadata=…` pointing there. Everything is
off until an administrator turns on **Admin › Settings › Agent access (MCP)**
(`mcp_enabled`); until then both paths answer 404.

**nginx.** `location /mcp/` turns `proxy_buffering` and `proxy_cache` off and
sets 300 s read/send timeouts — a tool call is one request whose answer must
not sit in a buffer — and sets no `proxy_set_header`, so it inherits the
server-level headers. `location = /mcp` covers the URL without the slash, and
`location /.well-known/oauth-protected-resource` sends discovery to the API
instead of the SPA. The Vite dev proxy forwards the same paths to `:8080`. A
proxy in front of nginx must pass all three.

**Logto.** Agents identify themselves with Client ID Metadata Documents
(CIMD, Logto ≥ 1.43) instead of a pre-registered app, and present Logto JWT
access tokens whose `aud` is `${PP_EXTERNAL_URL}/mcp/`; no scopes or roles are
involved. `pp-logto-setup.sh` converges both prerequisites: step 7 creates the
API resource `pp-hub MCP` with indicator `$PP_EXTERNAL_URL/mcp/` (found again
by indicator), step 8 sends `PATCH /api/configs/cimd {"enabled":true}`. Logto
refuses CIMD with **422** unless its SSRF protection is on with no allowlist —
`SSRF_PROTECTION_DISABLED`, `OIDC_PROVIDER_SSRF_PROTECTION_DISABLED` and
`SSRF_ALLOWED_ADDRESSES` must all be unset, as `compose.logto.yml` leaves
them. The script warns on that 422 and continues, so the rest of Logto still
converges; agents then fail at sign-in. An install configured before M41 needs
the script run once more.

**Reachability.** The agent's servers, not the person's browser, make the
calls: Claude's and ChatGPT's servers must reach `/mcp/` and
`/.well-known/oauth-protected-resource*` on pp-hub **and** Logto's issuer
(discovery, authorize, token) from the internet, and Logto must be able to
make outbound HTTPS requests to fetch each client's CIMD document. An instance
behind an allowlist must admit Anthropic's egress range, `160.79.104.0/21`.
The per-person rate limit on `/mcp/` (1200/min, burst 200) is keyed on the
user rather than the address for the same reason: every Claude user arrives
from those few addresses.

### Outbound HTTP for link previews

A link bookmark in a block document (`POST /api/v1/nodes/{id}/unfurl`) makes
`server` fetch the linked page and its preview image from the public
internet, at an address a user typed. It is SSRF-hardened in code (`internal/unfurl`: destinations checked at
dial time, every redirect included, against private, loopback, link-local and
metadata ranges; `HTTP_PROXY`/`HTTPS_PROXY` ignored; http/https only; 5 s,
1 MiB of HTML, 5 MiB images), so no configuration is required. An install
without egress just gets bookmarks without a title or image; if egress is
filtered, allow outbound 80/443 from the `server` container for bookmarks to
work.

### Network segments

`compose.prod.yml` defines three networks rather than one flat default:

| Service          | Networks                 |
| ---------------- | ------------------------ |
| `web`            | `edge`                   |
| `server`         | `edge`, `data`, `office` |
| `worker`         | `data`, `office`         |
| `postgres`       | `data`                   |
| `onlyoffice`     | `office`                 |
| `seaweedfs`      | `data` (profile `s3`)    |
| `seaweedfs-init` | `data` (profile `s3`)    |

`server` is the only service on all three, because it is the only one
everything else has business talking to. The segment that matters is
`office`: OnlyOffice is a third-party service, publicly hostnamed, whose only
access control is a shared JWT secret, and it runs with
`ALLOW_PRIVATE_IP_ADDRESS=true` because its own fetcher has to reach `server`
at a private address. On one flat network that combination also let it open
connections to `postgres:5432` and `seaweedfs:8333`. It needs `server:8080`
and nothing else, so now that is all it can reach. The JWT gate is what stops
it being driven at all; the segmentation is what bounds the damage the day
that gate fails.

`web` also runs hardened, because it terminates every public request:
`no-new-privileges:true`, `cap_drop: ALL` with only `NET_BIND_SERVICE` added
back, and nginx's master process running as the `nginx` user rather than root
(the stock image drops only its workers). The unprivileged image variant is
not used because it changes the listen port and the whole topology assumes 80.

### `/wopi/` is not published

nginx deliberately has **no `location /wopi/`**. `PP_WOPI_SERVER_URL` is
`http://server:8080` inside compose, and that value is what becomes the
`WOPISrc` the Document Server fetches, so the browser never calls that path
and no deployment in this repository needs it reachable from outside.

Publishing it anyway turned the WOPI access token from a credential that only
mattered inside the network into an internet-wide bearer token — on the one
route group exempt from both the same-origin check and the rate limiter,
because the Document Server sends no `Origin` header and is authorized by the
signed token instead.

**If you run OnlyOffice outside this network** — its own host, a managed
Document Server, a second compose stack — then it cannot reach `server:8080`,
and you must do two things together: set `PP_WOPI_SERVER_URL` to a URL that is
publicly reachable, and re-add a `location /wopi/ { proxy_pass $api; }` in
`apps/web/nginx.conf`. Restrict it to the Document Server's address
(`allow`/`deny`, or your proxy's equivalent) — the token binds one node, one
subject and one rank re-resolved against live access, so a replay gains
nothing a session would not, but there is no reason to leave the route open to
the internet either. Doing one without the other gives an editor that opens
and then fails on `CheckFileInfo`.

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

### Profile fields from the IdP

Avatar (`picture`) and gender come with the `profile` scope and need no
configuration: they are mirrored onto the account at every sign-in, the avatar
downloaded by the server into blob storage — so the server, not only the
browser, must be able to reach wherever the IdP hosts avatars.

The phone number needs the `phone` scope, requested only with
`PP_OIDC_PHONE_SCOPE=1`. It is opt-in because a provider that has not granted
this client the scope answers `invalid_scope` and **every sign-in fails**,
rather than the phone simply staying blank. On Logto, a **third-party**
application must list `phone` under its _Permissions_ (user data) first; a
first-party one accepts it as is. With the Management API credentials set,
organization reconciles fill in phone numbers regardless of this flag.

### Hosting options

Both production stacks — pp-hub (`compose.prod.yml`) and Logto
(`compose.logto.yml`) — publish **no host ports**; they `expose` only.
Something in front terminates HTTPS and routes the four domains: `web:80`,
`onlyoffice:80`, `logto:3001` and `logto:3002`. Any of these works, and
`deploy/pp-generate.sh` produces the files for the first one directly.

#### A reverse proxy on the same host

`pp-generate.sh` writes `pphub.ports.yml` and `logto.ports.yml`, compose
overrides that publish the four services on `127.0.0.1` (8090, 9980, 3001,
3002), and a `Caddyfile` that routes the domains to them with automatic
certificates. Start each stack with its override (`-f pphub.compose.yml -f
pphub.ports.yml`) — see [self-hosting.md](self-hosting.md).

Another proxy works the same way. Mind its request-body limit on this hop:
nginx needs a `client_max_body_size` above the largest upload chunk, 64 MiB
(`0` is simplest; its 1 MB default answers 413 to every full-size chunk).
Caddy has no default limit. For Traefik, see the read-timeout note below.

#### Dokploy (or a similar platform)

Platforms that route domains to compose services themselves take both
compose files as they are. The notes below are written for Dokploy; Coolify
and Portainer differ only in where the settings live.

Dokploy's recommended flow is a **clean compose file** — no Traefik labels, no
`dokploy-network` — with domains assigned in the UI, which is how both files
are written: binding 80/443 would collide with Dokploy's Traefik.

1. Create a Compose application and paste `deploy/compose/compose.prod.yml`
   (source type **docker raw** works, and is the simplest: this file pulls,
   so no checkout has to reach the server).
2. Put the contents of `.env.prod.example` into the Environment tab, filled
   in — `PP_IMAGE_TAG` included.
3. Running the bundled SeaweedFS? Add **`COMPOSE_PROFILES=s3`** there too.
   Dokploy's deploy command is fixed and carries no `--profile`, and compose
   reads that variable from the env file. Without it, `PP_BLOB_BACKEND=s3`
   starts a server pointing at an endpoint nothing is serving.
4. GHCR is private by default: add a registry credential for
   `ghcr.io/pp-techs` (a PAT with `read:packages`), or make the three
   packages public. Without it the pull fails with `denied` and Dokploy
   reports a deploy error with no obvious cause.
5. In the Domains tab add two domains:

   | Service      | Container port | Example domain       |
   | ------------ | -------------- | -------------------- |
   | `web`        | 80             | `drive.example.com`  |
   | `onlyoffice` | 80             | `office.example.com` |

6. Deploy, then run the backfill once (below).

Do not bind ports 80 or 443 in the compose file — they belong to Traefik.

Dokploy always deploys with `up -d --build`. That is why `build:` lives in
`compose.build.yml` and not in `compose.prod.yml`: a build section would take
precedence over the pinned tag, and on a raw application — where the compose
file is the only thing copied to the server — the build fails outright with
`unable to prepare context: path "/etc/dokploy/compose/apps/server" not
found`. With no build section, `--build` is a no-op and the tag is pulled.

Subsequent deploys are a `PP_IMAGE_TAG` change in the Environment tab and a
redeploy.

**Traefik's read timeout — optional since M30.** Since v2.11.2/v3.0,
Traefik's entry points default `transport.respondingTimeouts.readTimeout` to
`60s`, and that bounds reading the _whole_ request, body included. Before M30
a file went up as one multipart POST, so any upload still arriving at the
60-second mark was cut off, and raising this timeout was required. Uploads
are now tus: the browser sends a file as a series of `PATCH` requests, each
sized from measured throughput to take about 15 seconds (256 KiB–64 MiB), and
a chunk that is cut off anyway loses only the bytes that never arrived — the
client asks the server for its offset and carries on. So the 60 s default no
longer bounds the size of a file. Change it only if a link is so slow that
even a 256 KiB chunk cannot finish in 60 s (below roughly 4 KiB/s), or for
some other long request of your own behind the same Traefik.

What a cut-off request looks like is still worth recognising: the transfer
tray now retries these statuses instead of failing, so a steady run of them in
the access log is how a too-tight timeout shows itself. It depends on how the
request reached Traefik — `499 Client Closed Request` over plain HTTP, `504`
over HTTPS/1.1 and HTTP/3, `502` over HTTP/2 (measured on Traefik 3.7).
Progress can run ahead of the server: it counts bytes handed to the OS socket
buffer, not bytes the server received, which is why the client resynchronises
with a `HEAD` after a failure. Time spent _after_ the body is in (blob write,
S3) is not bounded by the read timeout. Nothing in the compose file can change
it: entry points are Traefik's static config. To raise it anyway, in Dokploy,
**Web Server → Traefik → Modify** (`/etc/dokploy/traefik/traefik.yml`), add
to both entry points and restart Traefik:

```yaml
entryPoints:
  web:
    address: :80
    transport:
      respondingTimeouts:
        readTimeout: 0s
  websecure:
    address: :443
    transport:
      respondingTimeouts:
        readTimeout: 0s
    # …keep the existing http/http3/tls settings
```

`0s` is not an open door for slow-body clients: the `web` container's nginx
still drops a body that stalls for 60 s (`client_body_timeout`), and
`PP_MAX_UPLOAD_BYTES` still caps an upload's size. A finite value works too
if it comfortably exceeds the slowest expected chunk.

Behind Cloudflare's proxy (orange cloud) a cut-off request shows up as a `499`
with `Server: cloudflare` and a 21-byte body (`Client Closed Request` —
Traefik's text, relayed). The 499 itself says Cloudflare reaches the origin
over plain HTTP, i.e. SSL/TLS mode **Flexible**, on the `web` entry point. The
`web` container's access log records the same request as `400 0`, the
truncated body. Cloudflare also caps a request body at 100 MB on Free/Pro
plans and answers `413` above that regardless of `PP_MAX_UPLOAD_BYTES` —
which, since M30, no upload request reaches: the largest chunk the client
sends is 64 MiB, so a file of any size up to the ceiling passes through.

If you do raise it: `readTimeout` has no per-router form, so the change above
applies to every app behind that Traefik. To confine it to this deployment,
leave the entry points alone and take this domain's HTTPS out of Traefik's
HTTP layer with a TCP router, which the read timeout does not apply to (a file
under **Web Server → Traefik → File System**, `/etc/dokploy/traefik/dynamic/`):

```yaml
tcp:
  routers:
    pphub-https:
      entryPoints: [websecure]
      rule: "HostSNI(`drive.example.com`)"
      service: pphub-web
      tls:
        certResolver: letsencrypt
  services:
    pphub-web:
      loadBalancer:
        servers:
          - address: "<web container name>:80" # docker ps | grep web
```

Traefik then only terminates TLS and pipes bytes to the `web` container, whose
nginx applies its own limits. It needs TLS on the hop into Traefik:
Cloudflare's SSL/TLS mode must be **Full (strict)**, because on a plain-HTTP
entry point a TCP router can only match ``HostSNI(`*`)``, which would capture
every app on port 80. Keep the domain in Dokploy's Domains tab so port 80 still
serves the ACME challenge; a TCP router with a matching `HostSNI` takes
precedence over the HTTP router on 443.

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
cp .env.example .env          # once, then fill in PP_OIDC_* and PP_LOGTO_*
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
It is now also what the same-origin check compares against, so pointing it at
`http://localhost:8080` while browsing the Vite server on `:5173` makes every
write answer 403 `cross-origin request refused` while reads keep working —
which looks like a permissions bug and is not one. Set it to the address in
your browser's URL bar.

The other new variables need nothing in development. `PP_TRUSTED_PROXIES`
should stay unset: there is no proxy in front of `go run ./cmd/server`, and
the Vite dev server is not one either — it proxies to `:8080` from the same
machine, so the TCP peer is already the right answer.
`PP_MAX_UPLOAD_BYTES` defaults to 5G and `PP_AUDIT_RETENTION_DAYS` to 365.
`PP_BOOTSTRAP_ADMIN_EMAIL` is worth setting to your own address on a fresh
local database — it is the only way to get an admin console — but your IdP
has to assert `email_verified` for that address, or the promotion is refused
and the reason is in the server log.

**The `PP_LOGTO_*` variables are required in development too.** Without
`PP_LOGTO_ENDPOINT` and the two M2M credentials the server refuses to start,
naming what is missing. The quickest tenant is `--profile idp` (below): create
the admin account and one bootstrap M2M application in the console on
`http://localhost:3002`, then run `deploy/logto/pp-logto-setup.sh` with
`PP_EXTERNAL_URL=http://localhost:5173`, `SMTP_HOST=host.docker.internal
SMTP_PORT=1025` (the `mail` profile's Mailpit) and
`PP_ENV_FILE=apps/server/.env` — the README has the exact command.
`PP_LOGTO_ENDPOINT` is `http://localhost:3001` — no
`/oidc` — and `PP_LOGTO_API_RESOURCE` can stay at its default
`https://default.logto.app/api`, which is an identifier and not somewhere
anything connects. Spaces are then created from the app by an instance admin,
which creates their organizations in Logto; a space left over from an older
database is linked with **Link to Logto** on its People pane.

Two development-only wrinkles. **No webhook reaches a development server**:
Logto Cloud cannot reach `localhost`, and a local Logto refuses to deliver to
`host.docker.internal` or any other private address. Expose the server through
a public tunnel and point the webhook at that if you need it;
without it, membership still converges, just at sign-in and on the hourly
ticker rather than instantly. And **the two consent scopes are per
application**: an app registered before this feature existed has neither, and
adding them to the wrong one of several dev applications looks exactly like a
broken reconciler.

If port 5432 is already taken on your host, set `PP_POSTGRES_PORT` in
`deploy/compose/.env` and use the same port in `PP_DATABASE_URL`.

Optional pieces are behind profiles, so the default `up` stays light:

| Profile  | Adds                     | For                                    |
| -------- | ------------------------ | -------------------------------------- |
| _(none)_ | postgres, onlyoffice     | Everyday work                          |
| `worker` | preview worker           | PDF / video / office thumbnails        |
| `s3`     | SeaweedFS + bucket init  | Developing against the S3 blob backend |
| `idp`    | Logto + its own Postgres | Working without a Logto Cloud tenant   |
| `mail`   | Mailpit (SMTP catcher)   | Testing outgoing share emails          |

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

**Health and readiness.** `/healthz` is a constant 200 — "is the process
up", which is what a restart policy needs — and it is deliberately not what
either compose healthcheck uses. Both `server`'s image `HEALTHCHECK` and the
compose healthcheck point at `GET /readyz`, which pings Postgres and calls
`blob.Store.Check` with a 3s timeout and answers 503 if either fails, so a
container that cannot reach its database or its blob root is reported
unhealthy rather than healthy. `cmd/worker` has no listener at all; it
touches `/tmp/worker-healthy` every 30s while both dependencies check out
and removes it when they do not, and its healthcheck reads the file's
freshness with `find -mmin -2`.

**Backup and restore.** `deploy/backup/pp-backup.sh` dumps Postgres, then
archives the blob backend named by `PP_BLOB_BACKEND` — `local` tars
`/data/blobs` out of the `server` service, the bundled SeaweedFS profile
tars `/data` out of `seaweedfs`, and an external S3 bucket is refused with
an explanation (its own versioning/replication is the backup) rather than a
silent no-op — never the reverse order, verifies both artifacts before
calling itself done, and prunes backups past `-k` days (default 14).
`deploy/backup/pp-restore.sh` takes a backup directory, requires `--force`,
verifies the Postgres dump before stopping anything, restores both, and
polls `/readyz` for up to 60s. Neither script hardcodes a volume name —
`compose.prod.yml` carries no `name:`, so every volume is reached through
`docker compose run`/`exec` on the service that mounts it, not by guessing
the project-prefixed name Compose or Dokploy assigned it. `.env.prod` is
never part of the backup; keep it (and its secrets) separately, or a
restore comes up as a different instance. The blob archive excludes
`/data/blobs/.uploads`, the staging area of in-flight resumable uploads:
those are not files yet, and an upload whose restored row has no bytes behind
it is dropped on its next chunk, so the client starts that file over.

A home-grown backup should keep the same order — Postgres first, then the
blobs — and skip `.uploads/` and the store's `tmp-*` files. GC collects a blob
only an hour after its last reference goes (`released_at`), so every blob the
dump references is still there as long as the blob copy finishes within an
hour of the dump. The blob store is content-addressed and never rewrites a
file, so an incremental copy that only adds new names is enough.

**The server blocks on OIDC discovery** for up to two minutes at startup, then
exits and is restarted. `web` therefore depends on `server` for _start order
only_, never `condition: service_healthy` — otherwise an IdP outage takes the
whole site down, static bundle included. nginx resolves the API through
Docker's DNS per-request, so it stays up and returns 502 until the API is back.

**Storage.** `PP_BLOB_BACKEND=local` keeps blobs in a volume, which pins the
stack to one host. `s3` is what allows more than one `server` replica — for
downloads and saves; resumable uploads still assume one `server` process (see
below). The key layout is identical, so migrating is a plain copy:
`aws s3 sync ./data/blobs s3://<bucket>/` (skip `.uploads`). See
[storage.md](architecture/storage.md).

**Resumable uploads stage on the blob volume.** Chunks accumulate in
`<PP_BLOB_ROOT>/.uploads` (`/data/blobs/.uploads` in `compose.prod.yml`)
until the last one lands, **whichever backend is in use**, so the volume at
`PP_BLOB_ROOT` must be persisted even on `s3` — the shipped compose files
already mount it — or a restart throws away every half-finished upload. It
needs room for the uploads in flight, each up to `PP_MAX_UPLOAD_BYTES`. With
`local` the finished file is renamed into the store, not copied — provided
`.uploads` is on the same filesystem, as the shipped compose files arrange;
mounted elsewhere, each commit falls back to copying the file. An upload
idle for 24 hours is swept with its bytes, every 15 minutes. The per-upload
lock that orders chunks is in-process and the staging files are local, so
the upload routes assume a single `server` process.

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

**OnlyOffice runs signed.** Both compose files set `JWT_ENABLED=true` with
`PP_ONLYOFFICE_JWT_SECRET`, which is a required secret in production and has a
fixed well-known default in development. The Document Server has no other
access control: without a signature anything that can reach its port can drive
its conversion API, and that API takes a URL to fetch. The worker is the only
process that calls it, and it signs every request; a mismatched secret shows
up as conversion error `-8` in the job's `last_error`.

The older instruction to keep JWT disabled was half right and half wrong: the
_conversion_ call used to be unsigned, so turning JWT on did break office
thumbnails. WOPI was never affected — the editor launch, `CheckFileInfo`,
`GetFile`, `PutFile` and the locks are authorized by the WOPI access token,
not by a JWT, and they work unchanged with `JWT_ENABLED=true` (verified
against 9.4.0).

**`ALLOW_PRIVATE_IP_ADDRESS` must stay enabled** in both compose files.
OnlyOffice's `request-filtering-agent` refuses to fetch from anything it
classifies as a private/link-local/meta address by default, and the WOPI host
it has to reach is exactly that: `host.docker.internal` in development
(OrbStack resolves it to `0.250.250.254`, which is why development _also_ sets
`ALLOW_META_IP_ADDRESS`) and `http://server:8080` in production, whose
container IP sits in `172.16.0.0/12`. Without it, every conversion fails with
the unhelpful `{"error":-4}`; the real reason
(`DNS lookup … is not allowed. Because, It is meta IP address.`) appears only
in `docker logs <onlyoffice container>`, never in the API or the job's
`last_error`.

**Production deliberately does not set `ALLOW_META_IP_ADDRESS`.** The range it
opens contains `169.254.169.254`, the cloud metadata endpoint, and nothing in
the production topology is reachable there — it is a private-range hop, not a
link-local one. Setting it would hand the Document Server's fetcher the one
address on the network worth stealing, for no gain. The signature above is
what keeps the remaining private-range allowance from being an open door; the
two go together and neither is sufficient alone.

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

`go.mod` replaces `github.com/reearth/ygo` with the patched copy in
`apps/server/third_party/ygo` (see its `PATCHES.md`), so the `deps` stage
copies `third_party/` before `go mod download`; a build context without it
fails to resolve the module.

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

**There are two `.dockerignore` files, and there have to be.** Docker reads
that file from the root of the _build context_, and the `server` and `worker`
images build with `context: ../../apps/server` — so the root one governs only
the web image. Without `apps/server/.dockerignore`, `COPY . .` in the server
Dockerfile was pulling `apps/server/.env` (which holds
`PP_OIDC_CLIENT_SECRET` and `PP_SESSION_KEY`) and `apps/server/data/blobs`
(which on a developer machine is real user file content) into the builder
stage. Neither reaches the final image — the runtime stages copy only the
compiled binary — but both are retained in the local build cache and exported
wholesale by any CI that pushes with `--cache-to`. Keep the two files in step.

**Resources.** Document Server wants 2–4 GB RAM and is by far the heaviest
service. The worker is bursty — it spawns `pdftoppm`/`ffmpeg` per job with a
30s timeout each.

## Known gaps

- **The rate limits are per process.** `/auth/*`, `/s/{token}` and `/api/v1`
  are limited per client IP, but the token buckets are in memory, so running
  `N` `server` replicas allows `N` times the published rate. Redis would fix
  it and would be the only infrastructure this stack does not otherwise need.
  See [The security and limit variables](#the-security-and-limit-variables).
- **No per-user upload quota.** `PP_MAX_UPLOAD_BYTES` bounds one upload
  and `spaces.quota_bytes` bounds a space (and defaults to unlimited); there
  is nothing between them, so a member can make many maximum-sized uploads
  into a space whose quota was never set.
- **`style-src 'unsafe-inline'` in the CSP.** The only loose directive that is
  not load-bearing for a feature; removing it needs changes in `apps/web`,
  not in the deployment.
- **The notification stream and the collaboration rooms are per process.**
  Both live in the `server` process's memory; a second replica would host
  rooms and deliver notifications the first cannot see. Run one.
- **Read auditing is deduplicated, not exhaustive.** `security_events` records
  intentional reads (downloads, folder zips, inline views, editor opens) at
  most once per person, file and kind per hour, per replica; thumbnails and
  the editor's own byte fetches are not reads. See
  [production-readiness.md](architecture/production-readiness.md).
- **Security alerting has one rule.** `internal/alert` flags a burst of failed
  sign-ins from one address (by default 10 within 15 minutes) with a
  `security_alert` row, `pphub_security_alerts_total` and an email to every
  instance admin — but only when SMTP is configured, and there is no rule
  keyed on an account, because failed sign-ins mostly carry none.
- **No WOPI proof-key verification.** The Document Server's calls into
  `/wopi/files/...` are authorized by our own access token, not by proving
  they came from the Document Server. Deliberate: the token binds one node,
  one subject and one rank, and the rank is re-resolved against live access on
  every call, so replaying it gains nothing.
