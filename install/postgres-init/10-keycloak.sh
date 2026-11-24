#!/bin/sh
# First boot of the bundled PostgreSQL only (docker-entrypoint-initdb.d): the database of the bundled
# Keycloak, with its own login. The platform database is created later by the installation wizard.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v password="$KEYCLOAK_DB_PASSWORD" <<'SQL'
CREATE ROLE keycloak LOGIN PASSWORD :'password';
CREATE DATABASE keycloak OWNER keycloak ENCODING 'UTF8';
SQL
