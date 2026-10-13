#!/bin/sh
# pp-restore.sh — restore Postgres and blob storage for pp-hub from a
# backup directory produced by pp-backup.sh.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
COMPOSE=${COMPOSE:-$ROOT/deploy/compose/compose.prod.yml}
ENVFILE=${ENVFILE:-$ROOT/deploy/compose/.env.prod}
FORCE=0
BACKUP_DIR=""

usage() {
	echo "usage: $0 [-f compose-file] [-e env-file] --force <backup-dir>" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	-f)
		[ $# -ge 2 ] || usage
		COMPOSE=$2
		shift 2
		;;
	-e)
		[ $# -ge 2 ] || usage
		ENVFILE=$2
		shift 2
		;;
	--force)
		FORCE=1
		shift
		;;
	-*)
		usage
		;;
	*)
		if [ -n "$BACKUP_DIR" ]; then
			usage
		fi
		BACKUP_DIR=$1
		shift
		;;
	esac
done

[ -n "$BACKUP_DIR" ] || usage

if [ "$FORCE" -ne 1 ]; then
	echo "refusing to restore without --force." >&2
	echo "this would DESTROY the current 'pphub' Postgres database contents and the current /data/blobs contents, replacing them with the contents of: $BACKUP_DIR" >&2
	exit 1
fi

dc() {
	docker compose -f "$COMPOSE" --env-file "$ENVFILE" "$@"
}

PP_BLOB_BACKEND=$(grep -E '^PP_BLOB_BACKEND=' "$ENVFILE" | cut -d= -f2- | tr -d '"')
PP_BLOB_BACKEND=${PP_BLOB_BACKEND:-local}

DIR_ABS=$(CDPATH= cd -- "$BACKUP_DIR" && pwd)

if [ ! -f "$DIR_ABS/postgres.dump" ]; then
	echo "backup directory $DIR_ABS does not contain postgres.dump" >&2
	exit 1
fi

# 1. Verify the archive BEFORE touching anything.
if ! dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh postgres \
	-c 'pg_restore --list /backup/postgres.dump >/dev/null'; then
	echo "postgres.dump failed verification (pg_restore --list) — aborting before touching any service" >&2
	exit 1
fi

# 2. Stop writers.
dc stop server worker

# 3. Restore Postgres. No --exit-on-error: --clean against a not-yet-populated
# schema legitimately emits "does not exist" notices for objects that were
# never created, and those must not abort the restore.
dc exec -T postgres pg_restore -U pphub -d pphub --clean --if-exists --no-owner <"$DIR_ABS/postgres.dump"

# 4. Restore blobs — detect which archive is present.
if [ -f "$DIR_ABS/blobs.tar.gz" ]; then
	dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh server \
		-c 'rm -rf /data/blobs/* && tar -xzf /backup/blobs.tar.gz -C /data/blobs'
elif [ -f "$DIR_ABS/seaweed.tar.gz" ]; then
	dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh seaweedfs \
		-c 'rm -rf /data/* && tar -xzf /backup/seaweed.tar.gz -C /data'
else
	echo "backup directory $DIR_ABS contains neither blobs.tar.gz nor seaweed.tar.gz — refusing to silently skip blob restore" >&2
	exit 1
fi

# 5. Bring services back up and wait for readiness.
dc up -d server worker

READY=0
ELAPSED=0
while [ "$ELAPSED" -lt 60 ]; do
	if dc exec -T server wget -qO- http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
		READY=1
		break
	fi
	sleep 2
	ELAPSED=$((ELAPSED + 2))
done

if [ "$READY" -ne 1 ]; then
	echo "server did not become ready within 60s after restore (Postgres/blob restore already completed — this is reporting the app did not come back healthy, not undoing anything)" >&2
	exit 1
fi

# 6. Report restored migration version.
RESTORED_MIGRATION=""
if [ -f "$DIR_ABS/manifest.txt" ]; then
	RESTORED_MIGRATION=$(grep -E '^migration_version=' "$DIR_ABS/manifest.txt" | cut -d= -f2-)
fi

echo "restore complete."
echo "restored migration version: ${RESTORED_MIGRATION:-unknown}"
echo "note: a dump older than the running image is fine — cmd/server applies the migration delta on start and cmd/worker's AwaitSchema waits for it."
