#!/usr/bin/env bash
# Run the backend directly with Podman host networking (no bridge).
# Works from Nushell with: ./scripts/podman-run.sh
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  podman build -t space-backend .
fi

podman rm -f space-backend >/dev/null 2>&1 || true

podman run -d --name space-backend --network host \
  --env-file .env \
  -e DATABASE_PATH=/app/data/firms.db \
  -e EXPORT_DIR=/app/exports \
  -e API_ADDR=:9999 \
  -v space-data:/app/data \
  -v space-exports:/app/exports \
  space-backend serve

echo
echo "Container started. Logs:"
podman logs --tail 20 space-backend

echo
echo "Health:"
for _ in $(seq 1 20); do
  if curl -fsS http://localhost:9999/health; then
    echo
    exit 0
  fi
  sleep 0.5
done

echo "API did not answer on :9999" >&2
podman logs --tail 50 space-backend >&2
exit 1
