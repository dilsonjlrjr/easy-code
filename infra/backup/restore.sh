#!/bin/sh
# Restores one backup run of backup.sh (RNF-11: RTO 4 h, restore tested every quarter; docs/dados/PERSIST.md):
# the dump into an empty database and the repositories into an empty folder. Checksums are verified first.
#
# Usage: restore.sh <run folder>
#   Database: PGHOST, PGPORT, PGUSER (a superuser of the new server: objects keep their owner roles, which the
#             first migration creates), PGPASSWORD, PGDATABASE (empty database). Skipped without PGDATABASE.
#   Repositories: EASYCODE_REPOS_DIR (empty folder of the new repos volume). Skipped without it.
# A dump restored outside production goes through the anonymization job before use (PERSIST.md).
set -eu

run="${1:?usage: restore.sh <run folder>}"
(cd "$run" && sha256sum -c --quiet SHA256SUMS)

if [ -n "${PGDATABASE:-}" ] && [ -f "$run/easycode.dump" ]; then
	pg_restore --no-password --exit-on-error --dbname="$PGDATABASE" "$run/easycode.dump"
fi
if [ -n "${EASYCODE_REPOS_DIR:-}" ] && [ -f "$run/repos.tar.gz" ]; then
	mkdir -p "$EASYCODE_REPOS_DIR"
	tar -C "$EASYCODE_REPOS_DIR" -xzf "$run/repos.tar.gz"
fi
echo "restored $run"
