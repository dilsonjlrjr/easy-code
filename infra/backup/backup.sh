#!/bin/sh
# Daily backup of Easy Code (RNF-11: RPO 24 h; docs/dados/PERSIST.md, backup): a logical dump of the
# PostgreSQL database in custom format, a copy of the git repositories of the projects and a copy of the files
# volume (attachments of the business need, US-03; ARQ section 9), in one folder per
# run under the destination, which must live outside the server. Runs taken more than
# EASYCODE_BACKUP_RETENTION_DAYS ago are removed (default and maximum 30 days, RNF-20, D-30).
#
# Usage: backup.sh <destination>
#   Database: PGHOST, PGPORT, PGUSER (easycode_backup in production), PGPASSWORD or PGPASSFILE, PGDATABASE.
#             Without PGDATABASE the database part is skipped.
#   Repositories: EASYCODE_REPOS_DIR (the repos volume, read-only). Without it the part is skipped.
#   Files: EASYCODE_FILES_DIR (the files volume, read-only). Without it the part is skipped.
# The private key of easycode-gateway is never copied here: it has its own backup (ADR-13).
set -eu

destination="${1:?usage: backup.sh <destination>}"
retention="${EASYCODE_BACKUP_RETENTION_DAYS:-30}"
if [ "$retention" -gt 30 ] || [ "$retention" -lt 1 ]; then
	echo "EASYCODE_BACKUP_RETENTION_DAYS must be between 1 and 30" >&2
	exit 2
fi
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
run="$destination/$stamp"
mkdir -p "$run.partial"

if [ -n "${PGDATABASE:-}" ]; then
	pg_dump --format=custom --no-password --file="$run.partial/easycode.dump" "$PGDATABASE"
	pg_restore --list "$run.partial/easycode.dump" >/dev/null # the dump is readable
fi
if [ -n "${EASYCODE_REPOS_DIR:-}" ]; then
	tar -C "$EASYCODE_REPOS_DIR" -czf "$run.partial/repos.tar.gz" .
fi
if [ -n "${EASYCODE_FILES_DIR:-}" ]; then
	tar -C "$EASYCODE_FILES_DIR" -czf "$run.partial/files.tar.gz" .
fi
(cd "$run.partial" && sha256sum ./* >SHA256SUMS)
mv "$run.partial" "$run" # a run only exists once complete

# Rotation: drop the runs older than the retention (folder names are UTC timestamps).
cutoff="$(date -u -d "@$(($(date -u +%s) - retention * 86400))" +%Y%m%dT%H%M%SZ)"
for old in "$destination"/*; do
	name="$(basename "$old")"
	case "$name" in
	*T*Z) if [ "$(expr "x$name" \< "x$cutoff")" = 1 ]; then rm -rf "$old"; fi ;;
	esac
done
echo "$run"
