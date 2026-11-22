#!/bin/sh
# Runs a command with a throwaway PostgreSQL (same image as compose.yaml) on a private Docker
# network, exporting EASYCODE_TEST_DATABASE_URL and TEST_NETWORK for "make test".
# Usage: scripts/with-postgres.sh make test
set -eu

image="${POSTGRES_IMAGE:-postgres:17.6-bookworm}"
suffix="$(date +%s)-$$"
network="easycode-ci-${suffix}"
container="easycode-ci-pg-${suffix}"

cleanup() {
	docker rm -f "$container" >/dev/null 2>&1 || true
	docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker network create "$network" >/dev/null
docker run -d --name "$container" --network "$network" --tmpfs /var/lib/postgresql/data \
	-e POSTGRES_USER=easycode -e POSTGRES_PASSWORD=easycode -e POSTGRES_DB=easycode \
	"$image" >/dev/null

# TCP check: during first-boot init the image runs a socket-only server.
tries=0
until docker exec "$container" pg_isready -q -h 127.0.0.1 -U easycode -d easycode; do
	tries=$((tries + 1))
	if [ "$tries" -ge 60 ]; then
		echo "postgres did not become ready" >&2
		docker logs "$container" >&2 || true
		exit 1
	fi
	sleep 1
done

export EASYCODE_TEST_DATABASE_URL="postgres://easycode:easycode@${container}:5432/easycode?sslmode=disable"
export TEST_NETWORK="--network ${network}"
"$@"
