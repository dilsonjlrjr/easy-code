#!/bin/sh
# Easy Code installation (US-55, ADR-22). Called by `make -C project install`.
# Paths may contain spaces: every path stays inside double quotes.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
env_file="$here/.env"
compose() { docker compose --env-file "$env_file" -f "$here/compose.yaml" ${COMPOSE_EXTRA:-} "$@"; }

random() { od -An -N24 -tx1 /dev/urandom | tr -d ' \n'; }

# 1. Passwords of the bundled PostgreSQL and Keycloak, created once and kept in install/.env (not in git).
if [ ! -f "$env_file" ]; then
	umask 077
	{
		echo "# Written by install/install.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ). Keep it: it holds the bundled passwords."
		echo "EASYCODE_BUNDLED=${EASYCODE_BUNDLED:-true}"
		echo "EASYCODE_BUNDLED_POSTGRES_PASSWORD=$(random)"
		echo "EASYCODE_BUNDLED_KEYCLOAK_DB_PASSWORD=$(random)"
		echo "EASYCODE_BUNDLED_KEYCLOAK_ADMIN=admin"
		echo "EASYCODE_BUNDLED_KEYCLOAK_ADMIN_PASSWORD=$(random)"
		echo "EASYCODE_DOCKER_GID=$(stat -c %g /var/run/docker.sock 2>/dev/null || echo 990)"
	} >"$env_file"
	echo "install/.env written with the bundled passwords."
fi

bundled="$(sed -n 's/^EASYCODE_BUNDLED=//p' "$env_file" | tail -n 1)"
profiles=""
if [ "${bundled:-true}" = "true" ]; then
	profiles="--profile bundled"
fi

# 2. Build the images once (the first build takes some minutes).
echo "Building the images..."
compose $profiles --profile install build

# 3. The wizard, unless the configuration volume is already locked.
if compose --profile install run --rm --no-deps installer status >/dev/null 2>&1; then
	echo "Easy Code is already installed: starting the platform."
else
	if [ -n "$profiles" ]; then
		echo "Starting the bundled PostgreSQL and Keycloak (Keycloak takes a few minutes the first time)..."
		compose $profiles up -d --wait postgres keycloak
	fi
	echo "Starting the installation wizard. Open the address below and type the token."
	compose --profile install run --rm --service-ports --no-deps installer
fi

# 4. The platform. The one-shot services (migrate, gateway-keys) run first; then the api answers /ready.
echo "Starting the platform..."
compose $profiles up -d
if command -v curl >/dev/null 2>&1; then
	tries=0
	until curl -fsS -o /dev/null "http://127.0.0.1:8081/api/v1/ready"; do
		tries=$((tries + 1))
		if [ "$tries" -ge 120 ]; then
			echo "The api is not ready after 10 minutes. See: docker compose -f install/compose.yaml logs api migrate" >&2
			exit 1
		fi
		sleep 5
	done
fi
echo ""
echo "Easy Code is up. Open the public web address you entered in the wizard (default http://localhost:8080)."
