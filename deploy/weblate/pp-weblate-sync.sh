#!/bin/sh
# pp-weblate-sync.sh — move translations between Weblate and the repository.
#
# Day to day nothing needs to run this: the component is configured with
# push_on_commit and commit_pending_age=1, so a translator's work reaches the
# `l10n/weblate` branch within about an hour on its own. This exists for the
# two moments where "about an hour" is not good enough.
#
#   ./pp-weblate-sync.sh push    before cutting a release — flush every pending
#                                edit to the branch NOW, so the release does
#                                not ship a catalog that is an hour stale
#
#   ./pp-weblate-sync.sh pull    after `lingui extract` lands on main — tell
#                                Weblate to fetch, so translators see the new
#                                source strings immediately instead of waiting
#                                for the daily sweep
#
#   ./pp-weblate-sync.sh status  what is pending, in both directions
#
# Applying what it pushed is a plain git operation, and deliberately so:
#
#   git fetch origin
#   git merge --ff-only origin/l10n/weblate
#
# `--ff-only` is not caution, it is an assertion. Weblate pushes its working
# branch under another name without force, so with merge_style=merge the
# branch is always a strict descendant of main and the merge is always a
# fast-forward. If it ever refuses, something changed underneath: the
# component's merge_style drifted to rebase, or git_force_push got turned on.
# Find out which rather than reaching for --no-ff.
#
# NEVER squash-merge this branch. Weblate identifies its own commits by SHA to
# decide what is already upstream; a squash replaces them with a new commit it
# cannot recognise, and it will keep trying to re-apply work that is already
# merged. Weblate's own docs are explicit about this.
set -eu

URL=${WEBLATE_URL:?set WEBLATE_URL, e.g. http://localhost:8090}
TOKEN=${WEBLATE_TOKEN:?set WEBLATE_TOKEN from /accounts/profile/#api}
PROJECT=${PP_WEBLATE_PROJECT:-pp-hub}
COMPONENT=${PP_WEBLATE_COMPONENT:-web}
URL=${URL%/}

REPO_API="$URL/api/components/$PROJECT/$COMPONENT/repository/"

status() {
	curl -sS --fail-with-body -H "Authorization: Token $TOKEN" "$REPO_API"
	echo
}

# Weblate serialises repository operations and answers 423 Locked while one is
# still settling, so a commit immediately followed by a push loses the race
# about as often as not. Retry rather than fail: the lock is transient by
# definition, and the alternative is a release script that needs a human to
# run it twice.
operation() {
	_try=0
	while [ "$_try" -lt 30 ]; do
		_out=$(curl -sS -o /dev/stdout -w '\n%{http_code}' -X POST \
			-H "Authorization: Token $TOKEN" \
			-H "Content-Type: application/json" \
			--data-binary "{\"operation\": \"$1\"}" \
			"$REPO_API")
		_code=$(printf '%s' "$_out" | tail -1)
		_body=$(printf '%s' "$_out" | sed '$d')
		case "$_code" in
		2*)
			printf '%s\n' "$_body"
			return 0
			;;
		423)
			_try=$((_try + 1))
			sleep 2
			;;
		*)
			printf '%s\n' "$_body" >&2
			echo "$1 failed with HTTP $_code" >&2
			return 1
			;;
		esac
	done
	echo "$1: gave up waiting for the repository lock" >&2
	return 1
}

case "${1:-status}" in
push)
	# Order matters: pending edits live in Weblate's database until they are
	# committed, so pushing first would publish nothing and report success.
	echo "committing pending translations..."
	operation commit
	echo "pushing to l10n/weblate..."
	operation push
	echo "state:"
	status
	echo
	echo "Now apply them:"
	echo "  git fetch origin && git merge --ff-only origin/l10n/weblate"
	;;
pull)
	echo "fetching main into Weblate..."
	operation pull
	echo "state:"
	status
	;;
status)
	status
	;;
*)
	echo "usage: $0 [push|pull|status]" >&2
	exit 1
	;;
esac
