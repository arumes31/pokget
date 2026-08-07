#!/usr/bin/env bash
set -euo pipefail

# Git Bash otherwise rewrites container paths such as /tmp/pokget.dump into
# Windows host paths before invoking the Docker CLI. Linux runners ignore it.
export MSYS_NO_PATHCONV=1

image="${1:-pokget:verification}"
suffix="${GITHUB_RUN_ID:-local}-$$"
network="pokget-verify-${suffix}"
database_container="pokget-verify-db-${suffix}"
application_container="pokget-verify-app-${suffix}"
database_user="pokget_verify"
database_name="pokget_verify"
database_password="pokget-verify-only-${suffix}"

cleanup() {
  docker rm -f "${application_container}" "${database_container}" >/dev/null 2>&1 || true
  docker network rm "${network}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker image inspect "${image}" >/dev/null

runtime_user="$(docker image inspect --format '{{.Config.User}}' "${image}")"
if [[ "${runtime_user}" != "pokget" ]]; then
  echo "expected production image user pokget, got ${runtime_user:-<empty>}" >&2
  exit 1
fi

docker run --rm --entrypoint sh "${image}" -c '
  test "$(id -u)" -eq 10001
  test -x /app/main
  test -x /app/catalog
  test -f /app/static/js/scanner.js
  test -f /app/static/js/sw.js
  tesseract --version >/dev/null
  command -v chromium-browser >/dev/null || command -v chromium >/dev/null
'

docker network create "${network}" >/dev/null
docker run -d \
  --name "${database_container}" \
  --network "${network}" \
  --network-alias database \
  -e "POSTGRES_USER=${database_user}" \
  -e "POSTGRES_PASSWORD=${database_password}" \
  -e "POSTGRES_DB=${database_name}" \
  postgres:17-alpine >/dev/null

for _ in $(seq 1 60); do
  if docker exec "${database_container}" pg_isready -U "${database_user}" -d "${database_name}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${database_container}" pg_isready -U "${database_user}" -d "${database_name}" >/dev/null

docker run -d \
  --name "${application_container}" \
  --network "${network}" \
  -e APP_PORT=18066 \
  -e DB_HOST=database \
  -e DB_PORT=5432 \
  -e "DB_USER=${database_user}" \
  -e "DB_PASSWORD=${database_password}" \
  -e "DB_NAME=${database_name}" \
  -e DB_SSLMODE=disable \
  -e SESSION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  -e SECURE_COOKIES=false \
  -e CATALOG_ENABLED=false \
  -e CATALOG_IMAGES_ENABLED=false \
  -e OLLAMA_HOST=http://127.0.0.1:11434 \
  "${image}" >/dev/null

ready=false
for _ in $(seq 1 90); do
  if docker exec "${application_container}" wget -q --spider http://127.0.0.1:18066/health/ready >/dev/null 2>&1; then
    ready=true
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "${application_container}")" != "true" ]]; then
    docker logs "${application_container}" >&2
    exit 1
  fi
  sleep 1
done
if [[ "${ready}" != "true" ]]; then
  docker logs "${application_container}" >&2
  echo "production container did not become ready" >&2
  exit 1
fi

docker exec "${application_container}" wget -q --spider http://127.0.0.1:18066/health/live
docker exec "${application_container}" wget -qO- http://127.0.0.1:18066/sw.js | grep -q "pokget-"

migration_version="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT version FROM schema_migrations')"
migration_dirty="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT dirty FROM schema_migrations')"
if [[ "${migration_version}" != "28" || "${migration_dirty}" != "f" ]]; then
  echo "unexpected migration state: version=${migration_version}, dirty=${migration_dirty}" >&2
  exit 1
fi

# Reproduce a legacy installation whose migration ledger reached version 27
# even though migration 6/8 objects were absent. Restarting the application
# must apply migration 28 and restore every runtime dependency.
docker stop "${application_container}" >/dev/null
docker exec "${database_container}" psql -v ON_ERROR_STOP=1 -U "${database_user}" -d "${database_name}" -c '
  ALTER TABLE cards DROP COLUMN rarity;
  DROP TABLE price_alerts;
  DROP TABLE price_history;
  UPDATE schema_migrations SET version = 27, dirty = FALSE;
' >/dev/null
docker start "${application_container}" >/dev/null

ready=false
for _ in $(seq 1 90); do
  if docker exec "${application_container}" wget -q --spider http://127.0.0.1:18066/health/ready >/dev/null 2>&1; then
    ready=true
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "${application_container}")" != "true" ]]; then
    docker logs "${application_container}" >&2
    exit 1
  fi
  sleep 1
done
if [[ "${ready}" != "true" ]]; then
  docker logs "${application_container}" >&2
  echo "production container did not recover the drifted schema" >&2
  exit 1
fi

migration_version="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT version FROM schema_migrations')"
migration_dirty="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT dirty FROM schema_migrations')"
if [[ "${migration_version}" != "28" || "${migration_dirty}" != "f" ]]; then
  echo "schema reconciliation did not advance migration state: version=${migration_version}, dirty=${migration_dirty}" >&2
  exit 1
fi

schema_reconciled="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c \
  "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'cards' AND column_name = 'rarity') AND to_regclass('public.price_history') IS NOT NULL AND to_regclass('public.price_alerts') IS NOT NULL")"
if [[ "${schema_reconciled}" != "t" ]]; then
  echo "required card and price schema was not reconciled" >&2
  exit 1
fi

docker exec "${database_container}" pg_dump -U "${database_user}" -d "${database_name}" -Fc -f /tmp/pokget.dump
docker exec "${database_container}" createdb -U "${database_user}" pokget_restore
docker exec "${database_container}" pg_restore -U "${database_user}" -d pokget_restore /tmp/pokget.dump

source_tables="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")"
restored_tables="$(docker exec "${database_container}" psql -At -U "${database_user}" -d pokget_restore -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")"
restored_version="$(docker exec "${database_container}" psql -At -U "${database_user}" -d pokget_restore -c 'SELECT version FROM schema_migrations')"
if [[ "${source_tables}" -eq 0 || "${source_tables}" != "${restored_tables}" || "${restored_version}" != "28" ]]; then
  echo "backup/restore mismatch: source_tables=${source_tables}, restored_tables=${restored_tables}, restored_version=${restored_version}" >&2
  exit 1
fi

echo "production image, migration repair, and backup/restore smoke test passed"
