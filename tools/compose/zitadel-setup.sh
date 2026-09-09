#!/usr/bin/env bash
# zitadel-setup.sh — Creates the pphub-server OIDC application in Zitadel.
# Run this ONCE after `docker compose up` has fully started and Zitadel is
# reachable at http://localhost:9080.
#
# What it does:
#   1. Waits for the Zitadel API to be healthy.
#   2. Authenticates as the initial admin user.
#   3. Creates a "Web" OIDC application named "pphub-server" with PKCE + PKCE-S256.
#   4. Prints the generated client_id (and client_secret if authentication method
#      is "basic") so you can set them in docker-compose.yml or your environment.
#
# The redirect URI matches what the server uses in go-oidc:
#   cfg.ExternalURL + "/auth/callback"  →  http://localhost:8080/auth/callback

set -euo pipefail

ZITADEL_API="http://localhost:9080"
ADMIN_USER="admin"
ADMIN_PASSWORD="admin123!"

# ── helpers ────────────────────────────────────────────────────────────────────

log()  { echo "[zitadel-setup] $*"; }
die()  { echo "[zitadel-setup] ERROR: $*" >&2; exit 1; }

wait_for_zitadel() {
  log "Waiting for Zitadel API..."
  for i in $(seq 1 60); do
    if curl -sf "${ZITADEL_API}/.well-known/openid-configuration" > /dev/null 2>&1; then
      log "Zitadel API is ready."
      return 0
    fi
    echo -n "."
    sleep 2
  done
  die "Zitadel API did not become ready within 120 seconds."
}

jq_need() {
  command -v jq > /dev/null || die "jq is required but not installed. Install it with: brew install jq"
}

# ── authenticate ───────────────────────────────────────────────────────────────

get_tokens() {
  local resp
  resp=$(curl -s -X POST "${ZITADEL_API}/oauth/v2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=password" \
    -d "username=${ADMIN_USER}" \
    -d "password=${ADMIN_PASSWORD}" \
    -d "scope=openid email profile" \
    -d "client_id=zitadel-admin") || die "Failed to authenticate as admin."

  ACCESS_TOKEN=$(echo "$resp" | jq -r .access_token)
  if [ -z "$ACCESS_TOKEN" ] || [ "$ACCESS_TOKEN" = "null" ]; then
    die "Failed to get access token. Check admin credentials. Response: $resp"
  fi
  log "Authenticated as admin."
}

# ── create application ─────────────────────────────────────────────────────────
#
# Zitadel v4 API layout:
#   POST /management/v1/projects/{projectId}/apps
# Project ID for the default instance org is "users" (built-in project).
# If that fails, fall back to creating our own project.

create_application() {
  local project_id="${1:-}"
  [ -z "$project_id" ] && die "Must provide project_id."

  # Payload: PKCE (S256), Redirect http://localhost:8080/auth/callback
  local payload
  payload=$(cat <<PAYLOAD
{
  "name": "pphub-server",
  "oauthVersion": "OIDC",
  "devMode": true,
  "oidcApp": {
    "redirectUris": ["http://localhost:8080/auth/callback"],
    "logoutRedirectUris": ["http://localhost:8080"],
    "responseTypesList": ["CODE"],
    "grantTypesList": ["AUTHORIZATION_CODE", "REFRESH_TOKEN"],
    "authMethodType": "PKCE",
    "pkceChallengeMethod": "S256",
    "accessTokenType": "JWT",
    "idTokenRoleAssertion": true,
    "idTokenUserinfoAssertion": true,
    "clockSkewSeconds": 10
  }
}
PAYLOAD
)

  local resp
  resp=$(curl -s -X POST "${ZITADEL_API}/management/v1/projects/${project_id}/apps" \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$payload")

  CLIENT_ID=$(echo "$resp" | jq -r '.clientId // .client_id // empty')
  CLIENT_SECRET=$(echo "$resp" | jq -r '.clientSecret // .client_secret // empty')

  if [ -z "$CLIENT_ID" ] || [ "$CLIENT_ID" = "null" ]; then
    die "Failed to create application. Response: $resp"
  fi

  log "Application 'pphub-server' created."
  log "  client_id:     $CLIENT_ID"
  if [ -n "$CLIENT_SECRET" ] && [ "$CLIENT_SECRET" != "null" ]; then
    log "  client_secret: $CLIENT_SECRET"
  else
    log "  client_secret: (PKCE mode — no secret generated; public client)"
  fi
  echo ""
  echo "Update docker-compose.yml (or your environment) with:"
  echo "  PP_OIDC_CLIENT_ID=${CLIENT_ID}"
  echo "  PP_OIDC_CLIENT_SECRET=${CLIENT_SECRET:-}"
}

# ── find default project ───────────────────────────────────────────────────────
#
# Zitadel exposes the built-in "users" project at a fixed project ID.
# We query the admin API to retrieve it, then fall back to creating a project.

find_project() {
  # Try the users project first (UUID format in Zitadel v3+)
  local users_project="266647658823345153"

  log "Attempting to use built-in users project (${users_project})..."

  local test_resp
  test_resp=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    "${ZITADEL_API}/management/v1/projects/${users_project}")

  if [ "$test_resp" = "200" ]; then
    log "Found users project: ${users_project}"
    echo "$users_project"
    return 0
  fi

  log "Users project not found. Listing available projects..."
  local projects_resp
  projects_resp=$(curl -s -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    "${ZITADEL_API}/management/v1/projects")

  # Try to extract first project id from list response
  local first_id
  first_id=$(echo "$projects_resp" | jq -r '.details?.totalCount // 0' 2>/dev/null)

  if [ "$first_id" -gt 0 ] 2>/dev/null; then
    local proj_id
    proj_id=$(echo "$projects_resp" | jq -r '.projects[0].id // empty')
    if [ -n "$proj_id" ] && [ "$proj_id" != "null" ]; then
      log "Using project: ${proj_id}"
      echo "$proj_id"
      return 0
    fi
  fi

  die "Could not find or create a Zitadel project. Please open http://localhost:9080, log in as admin, create a project named 'pphub', and retry."
}

# ── main ───────────────────────────────────────────────────────────────────────

main() {
  jq_need
  wait_for_zitadel
  get_tokens
  local project_id
  project_id=$(find_project)
  create_application "$project_id"
}

main "$@"
