#!/usr/bin/env bash
# Ubuntu/Debian bootstrap for the dedicated training VM; idempotent.
set -Eeuo pipefail
: "${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD to the training DB password from the assignment}"
if (( EUID == 0 )); then SUDO=(); else SUDO=(sudo -n); fi
if ! command -v psql >/dev/null; then
  "${SUDO[@]}" apt-get update
  "${SUDO[@]}" env DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql postgresql-client curl
fi
"${SUDO[@]}" systemctl enable --now postgresql
"${SUDO[@]}" -u postgres psql -v ON_ERROR_STOP=1 -v "db_password=$POSTGRES_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE validator LOGIN PASSWORD %L', :'db_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname='validator')\gexec
SELECT 'CREATE DATABASE "project-sem-1" OWNER validator'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname='project-sem-1')\gexec
SQL
# PostgreSQL remains on loopback; no public database port is opened.
