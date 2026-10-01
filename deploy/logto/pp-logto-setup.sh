#!/bin/sh
# pp-logto-setup.sh — configure a Logto tenant so pp-hub works against it.
#
# pp-hub needs nine things from Logto, and each one fails in its own quiet
# way when it is missing (docs/deployment.md, "Logto: identity and
# membership"). Clicked together in a console, none of them is reviewable and
# none survives a rebuild. So they live here:
#
#   1. a machine-to-machine app "pp-hub server" holding the Management API
#      `all` permission              → PP_LOGTO_M2M_CLIENT_ID / _SECRET
#   2. a Traditional web app "pp-hub" with this install's redirect URIs
#                                    → PP_OIDC_CLIENT_ID / _SECRET
#   3. the organization roles `admin` and `member`
#   4. an SMTP email connector, with templates for verification codes and an
#      OrganizationInvitation template that renders {{link}}   (if SMTP_HOST)
#   5. email sign-up with verification, email + password sign-in (if SMTP_HOST)
#   6. a webhook on the six events pp-hub reconciles from
#                                    → PP_LOGTO_WEBHOOK_KEY
#   7. an API resource "pp-hub MCP" whose indicator is $PP_EXTERNAL_URL/mcp/,
#      the audience of the tokens agents present to the MCP server
#   8. Client ID Metadata Documents (CIMD) switched on, so Claude and ChatGPT
#      can sign in without being registered here first
#   9. the values above, printed — and written into PP_ENV_FILE when given
#
# Idempotent: every object is looked up by name first. Existing apps keep
# their secrets; redirect URIs, connector config, sign-in settings and the
# webhook's events and URL are brought back to what pp-hub needs. Safe to
# re-run after changing a domain or the SMTP password.
#
# It authenticates with a *bootstrap* machine-to-machine app you create once
# in the admin console (Applications → Machine-to-machine → assign the role
# "Logto Management API access"). pp-hub itself gets its own app (1), so the
# bootstrap one can be deleted afterwards.
#
#   export LOGTO_ENDPOINT=https://auth.example.com
#   export LOGTO_BOOTSTRAP_CLIENT_ID=...  LOGTO_BOOTSTRAP_CLIENT_SECRET=...
#   export PP_EXTERNAL_URL=https://drive.example.com
#   export SMTP_HOST=smtp.example.com SMTP_PORT=587 SMTP_USER=... \
#          SMTP_PASS=... SMTP_FROM='pp-hub <noreply@example.com>'
#   export PP_ENV_FILE=deploy/out/pphub.env                  # optional
#   ./deploy/logto/pp-logto-setup.sh
#
# Requires curl and jq. See docs/self-hosting.md for the whole walkthrough.
set -eu

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

# shellcheck source=../lib/pp-env.sh
. "$(dirname "$0")/../lib/pp-env.sh"

ENDPOINT=${LOGTO_ENDPOINT:?set LOGTO_ENDPOINT, the Logto base URL, e.g. https://auth.example.com}
BOOT_ID=${LOGTO_BOOTSTRAP_CLIENT_ID:?set LOGTO_BOOTSTRAP_CLIENT_ID (admin console → Applications → Machine-to-machine)}
BOOT_SECRET=${LOGTO_BOOTSTRAP_CLIENT_SECRET:?set LOGTO_BOOTSTRAP_CLIENT_SECRET}
APP_URL=${PP_EXTERNAL_URL:?set PP_EXTERNAL_URL, the address users type, e.g. https://drive.example.com}
# The Management API's resource indicator: this literal on self-hosted Logto,
# https://<tenant-id>.logto.app/api on Logto Cloud.
RESOURCE=${LOGTO_API_RESOURCE:-https://default.logto.app/api}
ADMIN_ROLE=${PP_LOGTO_ORG_ROLE_ADMIN:-admin}
WEBHOOK_URL=${PP_WEBHOOK_URL:-}
ENV_FILE=${PP_ENV_FILE:-}
SMTP_HOST=${SMTP_HOST:-}

ENDPOINT=${ENDPOINT%/}
APP_URL=${APP_URL%/}
WEBHOOK_URL=${WEBHOOK_URL:-$APP_URL/hooks/logto}

M2M_NAME="pp-hub server"
M2M_ROLE="pp-hub server: Management API"
WEB_NAME="pp-hub"
HOOK_NAME="pp-hub"
MCP_NAME="pp-hub MCP"
# Trailing slash kept: it must match, byte for byte, the resource the MCP
# server advertises in its RFC 9728 metadata.
MCP_INDICATOR="$APP_URL/mcp/"
# Everything pp-hub converges from: membership and organization name/deletion,
# and account suspension, deletion, address and name.
HOOK_EVENTS='["Organization.Membership.Updated","Organization.Data.Updated","Organization.Deleted","User.Data.Updated","User.SuspensionStatus.Updated","User.Deleted"]'

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# --- HTTP ---------------------------------------------------------------------

TOKEN=$(curl -sS -u "$BOOT_ID:$BOOT_SECRET" \
	--data-urlencode grant_type=client_credentials \
	--data-urlencode "resource=$RESOURCE" \
	--data-urlencode scope=all \
	"$ENDPOINT/oidc/token" | jq -r '.access_token // empty')
if [ -z "$TOKEN" ]; then
	echo "could not get a Management API token from $ENDPOINT/oidc/token." >&2
	echo "Check the bootstrap app's id/secret, that it holds the role" >&2
	echo "'Logto Management API access', and LOGTO_API_RESOURCE ($RESOURCE)." >&2
	exit 1
fi

# api_status METHOD PATH [JSON] — prints the HTTP status; the body is left in
# $TMP/body. For the calls whose failure is not fatal.
api_status() {
	if [ $# -ge 3 ]; then
		curl -sS -o "$TMP/body" -w '%{http_code}' -X "$1" \
			-H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
			--data "$3" "$ENDPOINT/api$2"
	else
		curl -sS -o "$TMP/body" -w '%{http_code}' -X "$1" \
			-H "Authorization: Bearer $TOKEN" "$ENDPOINT/api$2"
	fi
}

# api METHOD PATH [JSON] — prints the body; exits naming the call on non-2xx.
api() {
	status=$(api_status "$@")
	case $status in
	2*) cat "$TMP/body" ;;
	*)
		echo "$1 $2 → $status: $(cat "$TMP/body")" >&2
		exit 1
		;;
	esac
}

# secret_of APP_ID — the app's current client secret. Logto moved secrets to
# /secrets; an app created before that still carries the legacy one.
secret_of() {
	s=$(api GET "/applications/$1/secrets" | jq -r '.[0].value // empty')
	[ -n "$s" ] || s=$(api GET "/applications/$1" | jq -r '.secret // empty')
	[ -n "$s" ] || s=$(api POST "/applications/$1/secrets" '{"name":"pp-hub"}' | jq -r '.value')
	printf '%s' "$s"
}

echo "logto: $ENDPOINT"

# --- 1. machine-to-machine app for the pp-hub server ---------------------------

scope_id=$(api GET "/resources?page_size=100" | jq -r --arg r "$RESOURCE" '.[] | select(.indicator == $r) | .id' |
	while read -r rid; do api GET "/resources/$rid/scopes" | jq -r '.[] | select(.name == "all") | .id'; done)
[ -n "$scope_id" ] || { echo "Management API scope 'all' not found for $RESOURCE" >&2; exit 1; }

role_id=$(api GET "/roles?page_size=100&type=MachineToMachine" | jq -r --arg n "$M2M_ROLE" '.[] | select(.name == $n) | .id')
if [ -z "$role_id" ]; then
	echo "role '$M2M_ROLE': creating"
	role_id=$(api POST /roles "$(jq -n --arg n "$M2M_ROLE" --arg s "$scope_id" \
		'{name: $n, description: "Lets the pp-hub server manage organizations, members and users.", type: "MachineToMachine", scopeIds: [$s]}')" | jq -r .id)
else
	echo "role '$M2M_ROLE': exists"
fi

m2m_id=$(api GET "/applications?page_size=100&types=MachineToMachine" | jq -r --arg n "$M2M_NAME" '.[] | select(.name == $n) | .id')
if [ -z "$m2m_id" ]; then
	echo "app '$M2M_NAME': creating"
	m2m_id=$(api POST /applications "$(jq -n --arg n "$M2M_NAME" \
		'{name: $n, type: "MachineToMachine", description: "pp-hub server → Logto Management API"}')" | jq -r .id)
else
	echo "app '$M2M_NAME': exists"
fi
if ! api GET "/applications/$m2m_id/roles?page_size=100" | jq -e --arg r "$role_id" 'any(.[]; .id == $r)' >/dev/null; then
	api POST "/applications/$m2m_id/roles" "$(jq -n --arg r "$role_id" '{roleIds: [$r]}')" >/dev/null
fi
m2m_secret=$(secret_of "$m2m_id")

# --- 2. the sign-in application --------------------------------------------------

oidc_meta=$(jq -n --arg u "$APP_URL" \
	'{redirectUris: [($u + "/auth/callback")], postLogoutRedirectUris: [$u]}')
web_id=$(api GET "/applications?page_size=100&types=Traditional" | jq -r --arg n "$WEB_NAME" '.[] | select(.name == $n) | .id')
if [ -z "$web_id" ]; then
	echo "app '$WEB_NAME': creating"
	web_id=$(api POST /applications "$(jq -n --arg n "$WEB_NAME" --argjson m "$oidc_meta" \
		'{name: $n, type: "Traditional", description: "pp-hub sign-in", oidcClientMetadata: $m}')" | jq -r .id)
else
	echo "app '$WEB_NAME': exists, setting redirect URIs"
	api PATCH "/applications/$web_id" "$(jq -n --argjson m "$oidc_meta" '{oidcClientMetadata: $m}')" >/dev/null
fi
web_secret=$(secret_of "$web_id")

# --- 3. organization roles --------------------------------------------------------

org_roles=$(api GET "/organization-roles?page_size=100")
for r in "$ADMIN_ROLE" member; do
	if printf '%s' "$org_roles" | jq -e --arg n "$r" 'any(.[]; .name == $n)' >/dev/null; then
		echo "organization role '$r': exists"
	else
		echo "organization role '$r': creating"
		desc="Member of the space."
		[ "$r" = "$ADMIN_ROLE" ] && desc="Administers the space: settings, people and groups."
		api POST /organization-roles "$(jq -n --arg n "$r" --arg d "$desc" '{name: $n, description: $d}')" >/dev/null
	fi
done

# --- 4 + 5. email: SMTP connector and sign-in experience --------------------------

if [ -n "$SMTP_HOST" ]; then
	SMTP_PORT=${SMTP_PORT:-587}
	SMTP_FROM=${SMTP_FROM:?set SMTP_FROM, e.g. 'pp-hub <noreply@example.com>'}
	# 465 is implicit TLS; everything else starts plain and upgrades.
	SMTP_SECURE=${SMTP_SECURE:-$([ "$SMTP_PORT" = 465 ] && echo true || echo false)}

	# One shape for every code email, and the invitation with {{link}} — which
	# pp-hub fills with {PP_EXTERNAL_URL}/auth/login?login_hint=<address>.
	# No {{inviter.*}}: pp-hub invites through its machine-to-machine app, so
	# there is no inviting user and the placeholder would be sent verbatim.
	code_mail() { # usageType subject lead
		jq -n --arg t "$1" --arg s "$2" --arg l "$3" '{
			usageType: $t, contentType: "text/html", subject: $s,
			content: ("<p>" + $l + "</p><p style=\"font-size:24px;font-weight:600;letter-spacing:4px\">{{code}}</p><p>The code expires in 10 minutes. If you did not ask for it, ignore this email.</p>")
		}'
	}
	templates=$(
		{
			code_mail Register "Your pp-hub sign-up code" "Use this code to finish creating your pp-hub account:"
			code_mail SignIn "Your pp-hub sign-in code" "Use this code to sign in to pp-hub:"
			code_mail ForgotPassword "Reset your pp-hub password" "Use this code to reset your pp-hub password:"
			code_mail Generic "Your pp-hub verification code" "Your pp-hub verification code:"
			code_mail UserPermissionValidation "Confirm it is you" "Use this code to confirm the change to your pp-hub account:"
			code_mail BindNewIdentifier "Confirm your new address" "Use this code to confirm your new email address:"
			code_mail MfaVerification "Your pp-hub 2-step code" "Your pp-hub 2-step verification code:"
			code_mail BindMfa "Set up 2-step verification" "Use this code to set up 2-step verification:"
			jq -n '{
				usageType: "OrganizationInvitation", contentType: "text/html",
				subject: "You are invited to {{organization.name}} on pp-hub",
				content: "<p>You have been invited to join <strong>{{organization.name}}</strong> on pp-hub.</p><p><a href=\"{{link}}\">Sign in or create your account</a> with this email address to join.</p><p>If you were not expecting this, ignore this email.</p>"
			}'
		} | jq -s .
	)
	smtp_config=$(jq -n \
		--arg host "$SMTP_HOST" --argjson port "$SMTP_PORT" \
		--arg user "${SMTP_USER:-}" --arg pass "${SMTP_PASS:-}" \
		--arg from "$SMTP_FROM" --argjson secure "$SMTP_SECURE" \
		--argjson templates "$templates" '{
			host: $host, port: $port, fromEmail: $from, secure: $secure,
			# A relay that authorizes by source takes no credentials, but the
			# connector requires an `auth` object and Logto strips empty ones —
			# `type` alone survives and makes nodemailer skip authentication.
			auth: (if $user == "" then {type: "login"} else {type: "login", user: $user, pass: $pass} end),
			templates: $templates
		}')

	smtp_id=$(api GET /connectors | jq -r '.[] | select(.connectorId == "simple-mail-transfer-protocol") | .id')
	# Logto allows one email connector; replace a different one rather than
	# failing on it.
	other=$(api GET /connectors | jq -r '.[] | select(.type == "Email" and .connectorId != "simple-mail-transfer-protocol") | .id')
	for id in $other; do
		echo "email connector $id: removing (replaced by SMTP)"
		api DELETE "/connectors/$id" >/dev/null
	done
	if [ -z "$smtp_id" ]; then
		echo "email connector SMTP: creating"
		api POST /connectors "$(jq -n --argjson c "$smtp_config" '{connectorId: "simple-mail-transfer-protocol", config: $c}')" >/dev/null
	else
		echo "email connector SMTP: updating"
		api PATCH "/connectors/$smtp_id" "$(jq -n --argjson c "$smtp_config" '{config: $c}')" >/dev/null
	fi

	# Invitees must be able to create the account an invitation waits for,
	# with a verified address — that is what pp-hub accepts invitations on.
	echo "sign-in experience: email sign-up with verification, email + password sign-in"
	api PATCH /sign-in-exp '{
		"signInMode": "SignInAndRegister",
		"signUp": {"identifiers": ["email"], "password": true, "verify": true},
		"signIn": {"methods": [{"identifier": "email", "password": true, "verificationCode": true, "isPasswordPrimary": true}]},
		"forgotPasswordMethods": ["EmailVerificationCode"]
	}' >/dev/null
else
	echo "SMTP_HOST unset: skipping the email connector and sign-in settings." >&2
	echo "  Invitations and email verification will not work until an email" >&2
	echo "  connector exists; re-run with SMTP_* set." >&2
fi

# --- 6. webhook --------------------------------------------------------------------

hook_body=$(jq -n --arg n "$HOOK_NAME" --arg u "$WEBHOOK_URL" --argjson e "$HOOK_EVENTS" \
	'{name: $n, events: $e, config: {url: $u}, enabled: true}')
hook_id=$(api GET "/hooks?page_size=100" | jq -r --arg n "$HOOK_NAME" '.[] | select(.name == $n) | .id')
if [ -z "$hook_id" ]; then
	echo "webhook '$HOOK_NAME' → $WEBHOOK_URL: creating"
	hook_id=$(api POST /hooks "$hook_body" | jq -r .id)
else
	echo "webhook '$HOOK_NAME' → $WEBHOOK_URL: updating"
	api PATCH "/hooks/$hook_id" "$hook_body" >/dev/null
fi
hook_key=$(api GET "/hooks/$hook_id" | jq -r .signingKey)

# --- 7. MCP API resource ------------------------------------------------------------

# Looked up by indicator, not name: the indicator is what tokens are bound to
# and Logto will not change it, so a renamed resource is renamed back.
mcp_id=$(api GET "/resources?page_size=100" | jq -r --arg i "$MCP_INDICATOR" '.[] | select(.indicator == $i) | .id')
if [ -z "$mcp_id" ]; then
	echo "API resource '$MCP_NAME' → $MCP_INDICATOR: creating"
	api POST /resources "$(jq -n --arg n "$MCP_NAME" --arg i "$MCP_INDICATOR" '{name: $n, indicator: $i}')" >/dev/null
else
	echo "API resource '$MCP_NAME' → $MCP_INDICATOR: updating"
	api PATCH "/resources/$mcp_id" "$(jq -n --arg n "$MCP_NAME" '{name: $n}')" >/dev/null
fi

# --- 8. Client ID Metadata Documents ------------------------------------------------

# Logto fetches each agent's metadata document from a URL the agent names, so
# it refuses (422) while its SSRF protection is off or loosened. Not fatal:
# everything else works, agent access just stays unavailable.
cimd_status=$(api_status PATCH /configs/cimd '{"enabled": true}')
case $cimd_status in
2*) echo "client ID metadata documents: enabled" ;;
422)
	echo "warning: Logto refused to enable client ID metadata documents (422): $(cat "$TMP/body")" >&2
	echo "warning: remove SSRF_PROTECTION_DISABLED, OIDC_PROVIDER_SSRF_PROTECTION_DISABLED and SSRF_ALLOWED_ADDRESSES from Logto's environment, restart it and re-run;" >&2
	echo "warning: until then agents (Claude, ChatGPT) cannot sign in to pp-hub over MCP." >&2
	;;
*)
	echo "PATCH /configs/cimd → $cimd_status: $(cat "$TMP/body")" >&2
	exit 1
	;;
esac

# --- 9. output --------------------------------------------------------------------

cat >"$TMP/env" <<EOF
PP_OIDC_ISSUER=$ENDPOINT/oidc
PP_OIDC_CLIENT_ID=$web_id
PP_OIDC_CLIENT_SECRET=$web_secret
PP_LOGTO_ENDPOINT=$ENDPOINT
PP_LOGTO_M2M_CLIENT_ID=$m2m_id
PP_LOGTO_M2M_CLIENT_SECRET=$m2m_secret
PP_LOGTO_API_RESOURCE=$RESOURCE
PP_LOGTO_WEBHOOK_KEY=$hook_key
PP_LOGTO_ORG_ROLE_ADMIN=$ADMIN_ROLE
EOF

if [ -n "$ENV_FILE" ]; then
	while IFS= read -r line; do
		pp_set_env "$ENV_FILE" "${line%%=*}" "${line#*=}"
	done <"$TMP/env"
	echo
	echo "Wrote the Logto values into $ENV_FILE."
else
	echo
	echo "Add these to pp-hub's environment:"
	echo
	cat "$TMP/env"
fi
