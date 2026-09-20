#!/bin/sh
# pp-backup.sh — back up Postgres and blob storage for pp-hub.
#
# Ordering rationale (do not reorder steps 2 and 3):
#   Postgres is dumped FIRST, blobs are archived SECOND. A blob written
#   between the two steps is harmless either way: if it lands in the blob
#   archive it is merely an extra, unreferenced file that a future GC pass
#   will reclaim; if it is missed by the blob archive but was never
#   referenced by the (already-taken) Postgres dump, nothing points at it.
#   The pair that must never invert is: a blob referenced by a *new*
#   Postgres row that made it into the dump, but whose bytes are missing
#   from the blob archive because the archive was taken before the row
#   existed. Dumping Postgres first and capturing blobs after guarantees
#   every blob referenced by the dump is already on disk when it is
#   archived.
#
# Suggested cron entry (NOT installed by this script):
#   15 2 * * * /srv/pp-hub/deploy/backup/pp-backup.sh >> /var/log/pp-backup.log 2>&1
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
COMPOSE=${COMPOSE:-$ROOT/deploy/compose/compose.prod.yml}
ENVFILE=${ENVFILE:-$ROOT/deploy/compose/.env.prod}
OUT="$ROOT/backups"
KEEP=14

usage() {
	echo "usage: $0 [-f compose-file] [-e env-file] [-o out-dir] [-k keep-days]" >&2
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
	-o)
		[ $# -ge 2 ] || usage
		OUT=$2
		shift 2
		;;
	-k)
		[ $# -ge 2 ] || usage
		KEEP=$2
		shift 2
		;;
	*)
		usage
		;;
	esac
done

dc() {
	docker compose -f "$COMPOSE" --env-file "$ENVFILE" "$@"
}

PP_BLOB_BACKEND=$(grep -E '^PP_BLOB_BACKEND=' "$ENVFILE" | cut -d= -f2- | tr -d '"')
PP_BLOB_BACKEND=${PP_BLOB_BACKEND:-local}

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
DIR="$OUT/$STAMP"
mkdir -p "$DIR"
DIR_ABS=$(CDPATH= cd -- "$DIR" && pwd)

# 1. Postgres first.
dc exec -T postgres pg_dump -U pphub -d pphub -Fc --clean --if-exists >"$DIR/postgres.dump"

# 2. Blobs second (see ordering rationale above).
BLOB_ARCHIVE=""
case "$PP_BLOB_BACKEND" in
s3)
	PP_S3_ENDPOINT=$(grep -E '^PP_S3_ENDPOINT=' "$ENVFILE" | cut -d= -f2- | tr -d '"')
	case "$PP_S3_ENDPOINT" in
	*seaweedfs*)
		dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh seaweedfs \
			-c 'tar -czf /backup/seaweed.tar.gz -C /data .'
		BLOB_ARCHIVE="seaweed.tar.gz"
		;;
	*)
		echo "blob backup for an external bucket is the bucket's own versioning/replication; this script will not pretend to have captured it" >&2
		exit 2
		;;
	esac
	;;
*)
	# local, or any other/unknown value — treat as local (matches config.go's default).
	dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh server \
		-c 'tar -czf /backup/blobs.tar.gz -C /data/blobs .'
	BLOB_ARCHIVE="blobs.tar.gz"
	;;
esac

# 3. Manifest.
MIGRATION=$(dc exec -T postgres psql -U pphub -d pphub -Atc 'SELECT max(version) FROM schema_migrations')
{
	echo "timestamp_utc=$STAMP"
	echo "compose_file=$COMPOSE"
	echo "blob_backend=$PP_BLOB_BACKEND"
	echo "migration_version=$MIGRATION"
	if command -v git >/dev/null 2>&1 && git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1; then
		echo "git_commit=$(git -C "$ROOT" rev-parse --short HEAD)"
	fi
} >"$DIR/manifest.txt"

# 4. Verify what was written.
if ! dc run --rm --no-deps --user root -T -v "$DIR_ABS:/backup" --entrypoint sh postgres \
	-c 'pg_restore --list /backup/postgres.dump >/dev/null'; then
	echo "backup verification FAILED: postgres.dump did not pass pg_restore --list" >&2
	exit 1
fi

if ! tar -tzf "$DIR/$BLOB_ARCHIVE" >/dev/null; then
	echo "backup verification FAILED: $BLOB_ARCHIVE did not pass tar -tzf" >&2
	exit 1
fi

# 5. Retention.
find "$OUT" -maxdepth 1 -type d -name '20*' -mtime +"$KEEP" -exec rm -rf {} +

# 6. Report.
echo "backup written to: $DIR"
for f in "$DIR"/*; do
	du -sh "$f"
done
echo "NOTE: .env.prod (POSTGRES_PASSWORD, PP_SESSION_KEY, OIDC secrets) is NOT included in this backup and must be preserved separately by the operator — a restore without it comes up as a different instance (different secrets/config)."
