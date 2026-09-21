#!/bin/sh
# pp-weblate-setup.sh — create the pp-hub project and component in Weblate.
#
# The component configuration is the part of the translation setup that is
# easy to get wrong and invisible once it is wrong: a `po-mono` file format,
# a `template` that should have been empty, a missing `icu-message-format`
# flag. Typed into a web form, none of that is reviewable and none of it
# survives a rebuild of the instance. So it lives here instead.
#
# Idempotent: it GETs each object first and skips what already exists. It
# never PATCHes, because reconciling a component someone deliberately tuned in
# the UI is a worse failure than telling them it is already there.
#
#   export WEBLATE_URL=http://localhost:8090
#   export WEBLATE_TOKEN=wlu_...        # /accounts/profile/#api
#   export PP_REPO=git@github.com:pp-techs/pp-hub.git
#   ./deploy/weblate/pp-weblate-setup.sh
#
# Before running, give Weblate read/write on the repository: copy its SSH key
# from Manage → SSH keys into the repository's Deploy keys with write access.
# GitHub accepts a given key on exactly one repository, so it cannot be a key
# already used elsewhere.
#
# See docs/architecture/i18n.md for why each value below is what it is.
set -eu

URL=${WEBLATE_URL:?set WEBLATE_URL, e.g. http://localhost:8090}
TOKEN=${WEBLATE_TOKEN:?set WEBLATE_TOKEN from /accounts/profile/#api}
REPO=${PP_REPO:?set PP_REPO, e.g. git@github.com:pp-techs/pp-hub.git}
BRANCH=${PP_BRANCH:-main}
WEB=${PP_WEB:-https://github.com/pp-techs/pp-hub}
PROJECT=pp-hub
COMPONENT=web

URL=${URL%/}

# `Token`, not `Bearer`: Bearer is only accepted from 2026.x onward and this
# has to work against both.
api() {
	# api <method> <path> [body]
	_method=$1
	_path=$2
	_body=${3:-}
	if [ -n "$_body" ]; then
		curl -sS --fail-with-body -X "$_method" \
			-H "Authorization: Token $TOKEN" \
			-H "Content-Type: application/json" \
			--data-binary "$_body" \
			"$URL/api$_path"
	else
		curl -sS --fail-with-body -X "$_method" \
			-H "Authorization: Token $TOKEN" \
			"$URL/api$_path"
	fi
}

exists() {
	# 200 = present, 404 = absent. Deliberately not "does POST 400?": a 400
	# is also what a bad filemask, an unknown file-format parameter or an
	# unknown language code produces, so branching on it would swallow real
	# configuration bugs.
	_code=$(curl -s -o /dev/null -w '%{http_code}' \
		-H "Authorization: Token $TOKEN" "$URL/api$1")
	[ "$_code" = "200" ]
}

echo "weblate: $URL"

if exists "/projects/$PROJECT/"; then
	echo "project $PROJECT: already exists"
else
	echo "project $PROJECT: creating"
	api POST /projects/ "$(
		cat <<-JSON
			{"name": "pp-hub", "slug": "$PROJECT", "web": "$WEB"}
		JSON
	)" >/dev/null
fi

if exists "/components/$PROJECT/$COMPONENT/"; then
	echo "component $PROJECT/$COMPONENT: already exists"
	exit 0
fi

echo "component $PROJECT/$COMPONENT: creating"
# Every field here is load-bearing:
#   file_format "po"   — bilingual gettext. "po-mono" would treat the English
#                        msgid as a key and lose source/translation separation.
#   template ""        — bilingual PO has no monolingual base. A non-empty
#                        template is how you get monolingual behaviour by
#                        accident.
#   new_base .pot      — what "Create new language file" starts from.
#   check_flags        — gettext components do not enable the ICU check on
#                        their own; without this, a translator can break
#                        {count, plural, ...} and Weblate will not notice.
#   language_regex     — applied with re.match, not fullmatch, so it is
#                        anchored by hand.
#   po_line_wrap 65535 — xgettext --no-wrap. Lingui writes one line per
#                        string; letting Weblate rewrap them makes every
#                        round trip a whole-file diff.
#   po_no_location false — Weblate defaults this to true, which strips the
#                        `#: file.tsx:12` lines. Those references are the only
#                        context a translator gets for a bare "Open" or
#                        "Cancel", so they are kept deliberately.
# `po_remove_obsolete` is deliberately absent: it does not exist before
# 2026.x and is a hard 400 there, and its default is already what we want.
RESP=$(api POST "/projects/$PROJECT/components/" "$(
	cat <<-JSON
		{
		  "name": "Web",
		  "slug": "$COMPONENT",
		  "vcs": "git",
		  "repo": "$REPO",
		  "push": "$REPO",
		  "branch": "$BRANCH",
		  "push_branch": "l10n/weblate",
		  "filemask": "apps/web/src/locales/*/messages.po",
		  "template": "",
		  "new_base": "apps/web/src/locales/messages.pot",
		  "file_format": "po",
		  "file_format_params": {"po_line_wrap": 65535, "po_no_location": false},
		  "new_lang": "add",
		  "check_flags": "icu-message-format",
		  "source_language": "en",
		  "language_regex": "^(en|vi)\$"
		}
	JSON
)")

# Creation is asynchronous — the clone and the first scan run in the
# background, which is where a bad deploy key or a filemask that matches
# nothing actually surfaces. Reporting success without waiting would mean
# reporting success for a component that never cloned.
TASK=$(printf '%s' "$RESP" | sed -n 's/.*"task_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -n "$TASK" ]; then
	printf 'waiting for the initial clone'
	i=0
	while [ "$i" -lt 60 ]; do
		printf '.'
		sleep 2
		if curl -sS -H "Authorization: Token $TOKEN" "$TASK" |
			grep -q '"completed"[[:space:]]*:[[:space:]]*true'; then
			break
		fi
		i=$((i + 1))
	done
	echo
fi

echo "repository status:"
api GET "/components/$PROJECT/$COMPONENT/repository/"
echo
echo "translations found:"
api GET "/components/$PROJECT/$COMPONENT/translations/" |
	sed -n 's/.*"language_code"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/  \1/p'
echo
echo "Done. If 'translations found' is empty, the filemask matched nothing —"
echo "check that $BRANCH really carries apps/web/src/locales/*/messages.po."
