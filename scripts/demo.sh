#!/usr/bin/env bash
# Populate the configured SQLite database with demo data and placeholder
# predictions. The API is NOT started; run the binary yourself afterwards.
#
#   ./scripts/demo.sh
#   # then:
#   ./bin/firms serve
#
# The placeholder probabilities are NOT ML results. They exist only to show the
# import/display path. The model label is "demo-placeholder-not-ml".
set -euo pipefail

cd "$(dirname "$0")/.."

BIN="${BIN:-./bin/firms}"
mkdir -p bin
if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  go build -o "$BIN" ./cmd/firms
fi
if [[ ! -x "$BIN" ]]; then
  echo "binary not found at $BIN; run without SKIP_BUILD=1 or set BIN" >&2
  exit 1
fi

echo "== apply migrations =="
"$BIN" init-db

echo "== sample ingestion =="
"$BIN" ingest-file --source MODIS_SP --file testdata/modis_sample.csv
"$BIN" ingest-file --source VIIRS_SNPP_SP --file testdata/viirs_sample.csv

echo "== matching (1.5 km / 30 min) =="
RUN_JSON="$("$BIN" harmonize --start 2024-01-01 --end 2024-01-03 --max-distance 1.5 --max-time 30)"
printf '%s\n' "$RUN_JSON"
RUN_ID="$(printf '%s' "$RUN_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run_id"])')"

echo "== placeholder predictions =="
"$BIN" demo-predictions --run-id "$RUN_ID" --model demo-placeholder-not-ml

cat <<EOF

Database populated.

  Start the API with:  $BIN serve
  Port:                API_ADDR from .env / environment (default :9999)
  Frontend:            NEXT_PUBLIC_API_URL=http://localhost:9999 pnpm dev --port 3100

Predictions in this demo are placeholders with model label
"demo-placeholder-not-ml". They demonstrate the import/display path only.
EOF
