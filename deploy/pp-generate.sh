#!/bin/sh
# pp-generate.sh — produce everything a new pp-hub install needs: two compose
# stacks (Logto, pp-hub), their filled-in environments with fresh secrets, and
# what it takes to put them behind HTTPS.
#
#   PP_DOMAIN=drive.example.com \
#   OFFICE_DOMAIN=office.example.com \
#   AUTH_DOMAIN=auth.example.com \
#   AUTH_ADMIN_DOMAIN=auth-admin.example.com \
#   PP_ADMIN_EMAIL=you@example.com \
#   PP_IMAGE_TAG=0.9.0 \
#   ./deploy/pp-generate.sh
#
# Writes deploy/out/ (git-ignored; override with OUT_DIR):
#
#   logto.compose.yml  logto.env    the Logto stack
#   pphub.compose.yml  pphub.env    the pp-hub stack
#   logto.ports.yml    pphub.ports.yml
#                                   publish the services on 127.0.0.1 for a
#                                   reverse proxy on the same host
#   Caddyfile                       that reverse proxy, with automatic HTTPS
#   NEXT-STEPS.md                   the steps, with your domains filled in
#
# The compose files publish no ports on their own, so they also paste as-is
# into a platform that routes domains itself (Dokploy, Coolify, Portainer…);
# there the *.ports.yml files and the Caddyfile are simply not used.
#
# The secrets are generated here and nowhere else, so it refuses to overwrite
# an existing output — regenerating would silently rotate the database
# passwords of a stack that is already running. FORCE=1 overrides that.
#
# pphub.env is left without the Logto application credentials: those exist
# only once Logto is running, and deploy/logto/pp-logto-setup.sh writes them
# into this same file. See docs/self-hosting.md.
set -eu

command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=lib/pp-env.sh
. "$ROOT/deploy/lib/pp-env.sh"

PP_DOMAIN=${PP_DOMAIN:?set PP_DOMAIN, the address users type, e.g. drive.example.com}
OFFICE_DOMAIN=${OFFICE_DOMAIN:?set OFFICE_DOMAIN, the document editor, e.g. office.example.com}
AUTH_DOMAIN=${AUTH_DOMAIN:?set AUTH_DOMAIN, Logto sign-in, e.g. auth.example.com}
AUTH_ADMIN_DOMAIN=${AUTH_ADMIN_DOMAIN:?set AUTH_ADMIN_DOMAIN, the Logto console, e.g. auth-admin.example.com}
PP_ADMIN_EMAIL=${PP_ADMIN_EMAIL:?set PP_ADMIN_EMAIL, the first instance administrator}
PP_IMAGE_TAG=${PP_IMAGE_TAG:-$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}
[ -n "$PP_IMAGE_TAG" ] || { echo "set PP_IMAGE_TAG, a released version such as 0.9.0" >&2; exit 1; }
OUT_DIR=${OUT_DIR:-$ROOT/deploy/out}

# Domains are hostnames; take them whether or not a scheme was typed.
strip() { printf '%s' "$1" | sed -E -e 's#^https?://##' -e 's#/+$##'; }
PP_DOMAIN=$(strip "$PP_DOMAIN")
OFFICE_DOMAIN=$(strip "$OFFICE_DOMAIN")
AUTH_DOMAIN=$(strip "$AUTH_DOMAIN")
AUTH_ADMIN_DOMAIN=$(strip "$AUTH_ADMIN_DOMAIN")

if [ -e "$OUT_DIR/pphub.env" ] && [ "${FORCE:-0}" != 1 ]; then
	echo "$OUT_DIR already holds a generated install; its secrets may be live." >&2
	echo "Remove it, or set FORCE=1 to regenerate every secret." >&2
	exit 1
fi
mkdir -p "$OUT_DIR"

# Hex for anything that ends up inside a URL (both database passwords are
# spliced into DB_URL / PP_DATABASE_URL); base64 where the consumer wants it.
hex() { openssl rand -hex "$1"; }
b64() { openssl rand -base64 32; }

# --- Logto -----------------------------------------------------------------------

cp "$ROOT/deploy/compose/compose.logto.yml" "$OUT_DIR/logto.compose.yml"
cat >"$OUT_DIR/logto.env" <<EOF
LOGTO_IMAGE_TAG=1.43.0
LOGTO_ENDPOINT=https://$AUTH_DOMAIN
LOGTO_ADMIN_ENDPOINT=https://$AUTH_ADMIN_DOMAIN
LOGTO_DB_PASSWORD=$(hex 24)
EOF

# --- pp-hub ----------------------------------------------------------------------

cp "$ROOT/deploy/compose/compose.prod.yml" "$OUT_DIR/pphub.compose.yml"
env=$OUT_DIR/pphub.env
cp "$ROOT/deploy/compose/.env.prod.example" "$env"
pp_set_env "$env" PP_IMAGE_TAG "$PP_IMAGE_TAG"
pp_set_env "$env" PP_EXTERNAL_URL "https://$PP_DOMAIN"
pp_set_env "$env" PP_ONLYOFFICE_URL "https://$OFFICE_DOMAIN"
pp_set_env "$env" POSTGRES_PASSWORD "$(hex 24)"
pp_set_env "$env" PP_SESSION_KEY "$(b64)"
pp_set_env "$env" PP_ONLYOFFICE_JWT_SECRET "$(b64)"
pp_set_env "$env" PP_BOOTSTRAP_ADMIN_EMAIL "$PP_ADMIN_EMAIL"
pp_set_env "$env" PP_OIDC_ISSUER "https://$AUTH_DOMAIN/oidc"
pp_set_env "$env" PP_LOGTO_ENDPOINT "https://$AUTH_DOMAIN"
pp_set_env "$env" PP_LOGTO_API_RESOURCE "https://default.logto.app/api"

# --- a reverse proxy on the same host ---------------------------------------------

cat >"$OUT_DIR/logto.ports.yml" <<'EOF'
# Publishes Logto on loopback for a reverse proxy on this host (see Caddyfile).
services:
  logto:
    ports:
      - "127.0.0.1:3001:3001"
      - "127.0.0.1:3002:3002"
EOF
cat >"$OUT_DIR/pphub.ports.yml" <<'EOF'
# Publishes pp-hub on loopback for a reverse proxy on this host (see Caddyfile).
services:
  server:
    ports:
      - "127.0.0.1:8090:8080"
  onlyoffice:
    ports:
      - "127.0.0.1:9980:80"
EOF
cat >"$OUT_DIR/Caddyfile" <<EOF
# Caddy fetches and renews the certificates itself; ports 80 and 443 must
# reach this host. No upload limits to raise: files go up in chunks of at
# most 64 MiB.
$PP_DOMAIN {
	reverse_proxy 127.0.0.1:8090
}

$OFFICE_DOMAIN {
	reverse_proxy 127.0.0.1:9980
}

$AUTH_DOMAIN {
	reverse_proxy 127.0.0.1:3001
}

$AUTH_ADMIN_DOMAIN {
	reverse_proxy 127.0.0.1:3002
}
EOF

# --- next steps ------------------------------------------------------------------

cat >"$OUT_DIR/NEXT-STEPS.md" <<EOF
# pp-hub — next steps

Generated for **https://$PP_DOMAIN**, image tag **$PP_IMAGE_TAG**.
The full walkthrough, with the reasons, is docs/self-hosting.md.

## 0. DNS

Point all four names at the server:

| Name | Used for | Service → port |
| --- | --- | --- |
| $PP_DOMAIN | pp-hub | \`server\` → 8080 |
| $OFFICE_DOMAIN | document editor | \`onlyoffice\` → 80 |
| $AUTH_DOMAIN | sign-in (Logto) | \`logto\` → 3001 |
| $AUTH_ADMIN_DOMAIN | Logto console | \`logto\` → 3002 |

## 1. Start Logto

With Docker Compose and Caddy on one host (run from this directory):

\`\`\`sh
docker compose -p logto -f logto.compose.yml -f logto.ports.yml --env-file logto.env up -d
caddy run --config Caddyfile     # or install it as a service
\`\`\`

On a platform that routes domains itself, create a compose app from
\`logto.compose.yml\` with \`logto.env\` as its environment, and map the two
Logto domains as in the table above.

Wait until https://$AUTH_DOMAIN/api/status answers, then open
https://$AUTH_ADMIN_DOMAIN:

1. Create the admin account.
2. **Applications → Create application → Machine-to-machine**, name it
   \`bootstrap\`, and on its **Roles** tab assign **Logto Management API
   access**. Copy its App ID and App secret.

## 2. Configure Logto for pp-hub

From the repository root:

\`\`\`sh
export LOGTO_ENDPOINT=https://$AUTH_DOMAIN
export LOGTO_BOOTSTRAP_CLIENT_ID=<App ID>
export LOGTO_BOOTSTRAP_CLIENT_SECRET=<App secret>
export PP_EXTERNAL_URL=https://$PP_DOMAIN
export SMTP_HOST=<smtp host> SMTP_PORT=587 SMTP_USER=<user> SMTP_PASS=<password>
export SMTP_FROM='pp-hub <noreply@$PP_DOMAIN>'
export PP_ENV_FILE=$env
./deploy/logto/pp-logto-setup.sh
\`\`\`

It fills the Logto values into \`pphub.env\`. The bootstrap app can be deleted
afterwards.

## 3. Start pp-hub

The images are public on ghcr.io/pp-techs (pp-hub-server, pp-hub-worker). From a private mirror, log
in first (\`docker login ghcr.io\` with a token that has \`read:packages\`).

\`\`\`sh
docker compose -p pphub -f pphub.compose.yml -f pphub.ports.yml --env-file pphub.env up -d
\`\`\`

On a platform: a second compose app from \`pphub.compose.yml\`, with the
filled-in \`pphub.env\`, and the two pp-hub domains mapped as above.

## 4. First sign-in

Open https://$PP_DOMAIN and **Create account** with $PP_ADMIN_EMAIL. That
address becomes the instance administrator; create the first space from the
space switcher and invite people from its People pane.

Upgrading later: change \`PP_IMAGE_TAG\` in \`pphub.env\` and run the same
\`up -d\`.
EOF

echo "Wrote $OUT_DIR:"
for f in "$OUT_DIR"/*; do echo "  ${f##*/}"; done
echo
echo "Next: $OUT_DIR/NEXT-STEPS.md"
