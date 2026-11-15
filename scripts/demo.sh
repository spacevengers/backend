#!/usr/bin/env bash
# Seed a self-contained demo database, attach clearly-labelled placeholder
# predictions, and start the API on http://localhost:8000.
#
#   ./scripts/demo.sh
#   NO_SERVE=1 ./scripts/demo.sh     # seed only
#
# The placeholder probabilities are NOT ML results. They exist only to show the
# import/display path. The model label is "demo-placeholder-not-ml".
set -euo pipefail

cd "$(dirname "$0")/.."

export DATABASE_PATH="${DATABASE_PATH:-./data/demo.db}"
export EXPORT_DIR="${EXPORT_DIR:-./exports}"
API_ADDR="${API_ADDR:-:8000}"
BIN="./bin/firms"
PREDICTIONS="./data/demo-predictions.json"
SERVER_LOG="./data/demo-server.log"

mkdir -p data exports bin
rm -f "$DATABASE_PATH" "$DATABASE_PATH-shm" "$DATABASE_PATH-wal"
go build -o "$BIN" ./cmd/firms

echo "== database =="
"$BIN" init-db

echo "== sample ingestion =="
"$BIN" ingest-file --source MODIS_SP --file testdata/modis_sample.csv
"$BIN" ingest-file --source VIIRS_SNPP_SP --file testdata/viirs_sample.csv

echo "== matching (1.5 km / 30 min) =="
RUN_JSON="$("$BIN" harmonize --start 2024-01-01 --end 2024-01-03 --max-distance 1.5 --max-time 30)"
printf '%s\n' "$RUN_JSON"
RUN_ID="$(printf '%s' "$RUN_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run_id"])')"

if [[ "${NO_SERVE:-0}" == "1" ]]; then
  echo "Seeded run $RUN_ID. Start with: $BIN serve"
  exit 0
fi

echo "== API =="
"$BIN" serve >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
cleanup() {
  kill "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

for _ in $(seq 1 30); do
  if curl -fsS "http://localhost${API_ADDR}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
if ! curl -fsS "http://localhost${API_ADDR}/health" >/dev/null 2>&1; then
  echo "API did not start; see $SERVER_LOG" >&2
  exit 1
fi

echo "== placeholder predictions =="
curl -fsS "http://localhost${API_ADDR}/api/integration/harmonized-observations?run_id=${RUN_ID}" \
  | python3 -c '
import json, sys
clusters = json.load(sys.stdin)
predictions = []
for cluster in clusters:
    frp = cluster.get("frp_viirs_sum")
    if cluster.get("match_status") != "matched" or frp is None:
        continue
    probability = min(0.95, max(0.05, 0.15 + 0.03 * float(frp)))
    predictions.append({
        "cluster_uid": cluster["cluster_uid"],
        "p_modis_detect": round(probability, 3),
        "frp_calibrated": round(float(frp) * 0.97, 2),
    })
json.dump({"model": "demo-placeholder-not-ml", "predictions": predictions}, sys.stdout)
' >"$PREDICTIONS"

"$BIN" import-predictions --run-id "$RUN_ID" --file "$PREDICTIONS" --model demo-placeholder-not-ml

cat <<EOF

Demo ready.

  Backend:  http://localhost${API_ADDR}
  Frontend: cd ../frontend
            NEXT_PUBLIC_API_URL=http://localhost${API_ADDR} pnpm dev --port 3100
  Open:     http://localhost:3100/map
  Select:   Backend · month, January 2024, Kenya

Predictions in this demo are placeholders with model label
"demo-placeholder-not-ml". They demonstrate the import/display path only.

Press Ctrl-C to stop the API.
EOF

wait "$SERVER_PID"
