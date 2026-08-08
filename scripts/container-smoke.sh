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
  css=/app/static/css/tailwind.css
  test -s "${css}"
  for selector in \
    ".min-h-dvh{" \
    ".max-w-md{" \
    ".mx-auto{" \
    ".w-16{" \
    ".h-16{" \
    ".grid-cols-2{" \
    ".px-4{" \
    ".text-3xl{"; do
    if ! grep -Fq "${selector}" "${css}"; then
      echo "production Tailwind bundle is missing template utility ${selector}" >&2
      exit 1
    fi
  done
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

database_ready=false
for _ in $(seq 1 60); do
  database_running="$(docker inspect --format '{{.State.Running}}' "${database_container}" 2>/dev/null || true)"
  if [[ "${database_running}" != "true" ]]; then
    docker logs "${database_container}" >&2 || true
    echo "PostgreSQL container exited before becoming ready" >&2
    exit 1
  fi

  # pg_isready can briefly succeed against the temporary server that the
  # official image starts while initializing a fresh data directory. Wait for
  # that phase to finish before accepting a successful readiness probe.
  if docker logs "${database_container}" 2>&1 |
      grep -Fq 'PostgreSQL init process complete; ready for start up.' &&
    docker exec "${database_container}" pg_isready \
      -U "${database_user}" -d "${database_name}" >/dev/null 2>&1; then
    database_ready=true
    break
  fi
  sleep 1
done
if [[ "${database_ready}" != "true" ]]; then
  docker logs "${database_container}" >&2 || true
  echo "PostgreSQL did not finish initialization and become ready" >&2
  exit 1
fi

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
service_worker="$(docker exec "${application_container}" wget -qO- http://127.0.0.1:18066/sw.js)"
if [[ "${service_worker}" != *pokget-* ]]; then
  echo "production service worker does not contain the expected cache name" >&2
  exit 1
fi

migration_version="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT version FROM schema_migrations')"
migration_dirty="$(docker exec "${database_container}" psql -At -U "${database_user}" -d "${database_name}" -c 'SELECT dirty FROM schema_migrations')"
if [[ "${migration_version}" != "28" || "${migration_dirty}" != "f" ]]; then
  echo "unexpected migration state: version=${migration_version}, dirty=${migration_dirty}" >&2
  exit 1
fi

# Reproduce a legacy installation whose migration ledger reached version 27
# even though card metadata, price, and catalog objects were absent. Restarting
# the application must apply migration 28 and pass runtime schema validation.
docker stop "${application_container}" >/dev/null
docker exec "${database_container}" psql -v ON_ERROR_STOP=1 -U "${database_user}" -d "${database_name}" -c '
  ALTER TABLE cards
    DROP COLUMN variant,
    DROP COLUMN change_24h,
    DROP COLUMN game,
    DROP COLUMN rarity,
    DROP COLUMN language,
    DROP COLUMN phash,
    DROP COLUMN source_id,
    DROP COLUMN source_card_id,
    DROP COLUMN set_code,
    DROP COLUMN collector_number,
    DROP COLUMN source_updated_at,
    DROP COLUMN source_metadata,
    DROP COLUMN catalog_active,
    DROP COLUMN first_seen_at,
    DROP COLUMN last_seen_at,
    DROP COLUMN last_seen_run_id,
    DROP COLUMN superseded_by_card_id;
  DROP TABLE price_alerts;
  DROP TABLE price_history;
  DROP TABLE catalog_printing_images;
  DROP TABLE card_fingerprints;
  DROP TABLE card_images;
  DROP TABLE catalog_printings;
  DROP TABLE catalog_source_state;
  DROP TABLE catalog_sync_runs;
  DROP TABLE catalog_sources;
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
  "WITH required(table_name, column_name) AS (
     VALUES
       ('cards', 'id'), ('cards', 'name'), ('cards', 'set_name'), ('cards', 'image_url'),
       ('cards', 'price_usd'), ('cards', 'price_eur'), ('cards', 'variant'), ('cards', 'change_24h'),
       ('cards', 'phash'), ('cards', 'game'), ('cards', 'language'), ('cards', 'rarity'),
       ('cards', 'source_id'), ('cards', 'source_card_id'), ('cards', 'set_code'),
       ('cards', 'collector_number'), ('cards', 'source_updated_at'), ('cards', 'source_metadata'),
       ('cards', 'catalog_active'), ('cards', 'first_seen_at'), ('cards', 'last_seen_at'),
       ('cards', 'last_seen_run_id'), ('cards', 'superseded_by_card_id'),
       ('price_history', 'card_id'), ('price_history', 'price_usd'),
       ('price_history', 'price_eur'), ('price_history', 'recorded_at'),
       ('price_alerts', 'id'), ('price_alerts', 'user_id'), ('price_alerts', 'card_id'),
       ('price_alerts', 'target_price'), ('price_alerts', 'is_active'),
       ('catalog_sources', 'id'), ('catalog_source_state', 'source_id'),
       ('catalog_sync_runs', 'id'), ('catalog_printings', 'id'),
       ('card_images', 'id'), ('card_fingerprints', 'image_id')
   )
   SELECT NOT EXISTS (
     SELECT 1
     FROM required
     LEFT JOIN information_schema.columns AS present
       ON present.table_schema = 'public'
      AND present.table_name = required.table_name
      AND present.column_name = required.column_name
     WHERE present.column_name IS NULL
   )")"
if [[ "${schema_reconciled}" != "t" ]]; then
  echo "required card, price, and catalog schema was not reconciled" >&2
  exit 1
fi

docker exec "${database_container}" pg_dump -U "${database_user}" -d "${database_name}" -Fc -f /tmp/pokget.dump
docker exec "${database_container}" createdb -U "${database_user}" pokget_restore
docker exec "${database_container}" pg_restore -U "${database_user}" -d pokget_restore /tmp/pokget.dump

restored_version="$(docker exec "${database_container}" psql -At -U "${database_user}" -d pokget_restore -c 'SELECT version FROM schema_migrations')"
if [[ "${restored_version}" != "28" ]]; then
  echo "backup/restore migration mismatch: restored_version=${restored_version}" >&2
  exit 1
fi

normalized_schema_dump() {
  local source_database="$1"
  local normalized_database="$2"

  docker exec "${database_container}" dropdb --if-exists -U "${database_user}" "${normalized_database}"
  docker exec "${database_container}" createdb -U "${database_user}" "${normalized_database}"
  docker exec "${database_container}" pg_dump \
    --schema-only --no-owner --no-privileges \
    -U "${database_user}" -d "${source_database}" |
    docker exec -i "${database_container}" psql -v ON_ERROR_STOP=1 \
      -U "${database_user}" -d "${normalized_database}" >/dev/null
  docker exec "${database_container}" pg_dump \
    --schema-only --no-owner --no-privileges \
    -U "${database_user}" -d "${normalized_database}" |
    sed -E '/^\\(un)?restrict /d'
}

source_schema="$(normalized_schema_dump "${database_name}" pokget_source_schema)"
restored_schema="$(normalized_schema_dump pokget_restore pokget_restored_schema)"
if [[ -z "${source_schema}" || -z "${restored_schema}" || "${source_schema}" != "${restored_schema}" ]]; then
  echo "backup/restore schema mismatch" >&2
  diff -u <(printf '%s\n' "${source_schema}") <(printf '%s\n' "${restored_schema}") >&2 || true
  exit 1
fi

echo "production image, migration repair, and backup/restore smoke test passed"
