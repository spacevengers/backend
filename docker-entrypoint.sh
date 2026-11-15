#!/bin/sh
set -eu

# SQLite needs write access to the database directory (for -wal/-shm files),
# not just the .db file. Named volumes may be created root-owned, so fix
# ownership once at startup and then drop to the unprivileged app user.
: "${DATABASE_PATH:=/app/data/firms.db}"
: "${EXPORT_DIR:=/app/exports}"

DATA_DIR="$(dirname "$DATABASE_PATH")"
mkdir -p "$DATA_DIR" "$EXPORT_DIR"

if [ "$(id -u)" = "0" ]; then
  chown -R firms:firms "$DATA_DIR" "$EXPORT_DIR" 2>/dev/null || true
  exec su-exec firms:firms firms "$@"
fi

exec firms "$@"
