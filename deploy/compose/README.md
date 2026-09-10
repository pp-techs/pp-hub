# Development Workflows for pp-hub

## Option A — Logto Cloud (recommended)

```bash
# 1. Start: PostgreSQL + OnlyOffice (no IdP container needed)
docker compose -f deploy/compose/compose.pg-oo.yml up -d

# 2. Create a Logto Cloud tenant (one-time, ~5 min, free):
#    a. Sign up at https://cloud.logto.io and create a tenant (Development)
#    b. Console > Applications > Create application > "Traditional Web"
#    c. In the app page, set:
#       Redirect URI:          http://localhost:8080/auth/callback
#       Post sign-out redirect URI: http://localhost:8080
#    d. Copy: issuer (https://<tenant-id>.logto.app/oidc), App ID, App Secret
#       into apps/server/.env (see step 3)

# 3. Server
cp deploy/compose/server.env apps/server/.env   # then fill PP_OIDC_* values
go run apps/server/cmd/server/main.go

# 4. Web
vp run dev   # open http://localhost:5173 and sign in
```

## Option B — Self-hosted Logto

```bash
# 1. Start: Logto + its PostgreSQL
docker compose -f deploy/compose/compose.logto.yml up -d

# 2. Start: app PostgreSQL + OnlyOffice
docker compose -f deploy/compose/compose.pg-oo.yml up -d

# 3. Wait ~30s (Logto runs DB migrations on first start), then:
#    Open http://localhost:3002 (Logto Admin Console)
#    - Create the admin account on first visit
#    - Applications > Create application > "Traditional Web"
#    - Redirect URI: http://localhost:8080/auth/callback
#    - Post sign-out redirect URI: http://localhost:8080

# 4. Server env: PP_OIDC_ISSUER=http://localhost:3001/oidc
#    plus App ID / App Secret from the console.
```

---

## Compose Files

| File                     | Services                                                           |
| ------------------------ | ------------------------------------------------------------------ |
| `compose.pg-oo.yml`      | postgres (app) + onlyoffice — main dev stack, use with Logto Cloud |
| `compose.logto.yml`      | logto + postgres (IdP) — self-hosted IdP option                    |
| `compose.onlyoffice.yml` | onlyoffice only                                                    |

---

## Environment Files

| File           | Purpose                                          |
| -------------- | ------------------------------------------------ |
| `server.env`   | Server env template (copy to `apps/server/.env`) |
| `.env.example` | Annotated example of all server env vars         |

---

## Useful Commands

```bash
# Start services
docker compose -f deploy/compose/compose.pg-oo.yml up -d

# Check status / logs / stop
docker compose -f deploy/compose/compose.pg-oo.yml ps
docker compose -f deploy/compose/compose.pg-oo.yml logs -f
docker compose -f deploy/compose/compose.pg-oo.yml down

# Reset data
docker compose -f deploy/compose/compose.pg-oo.yml down -v

# Health checks
pg_isready -h localhost -U postgres          # app postgres
curl http://localhost:9980/healthz           # onlyoffice
curl http://localhost:3001/api/status        # logto (self-hosted only)
curl https://<tenant-id>.logto.app/oidc/.well-known/openid-configuration  # logto cloud
```
