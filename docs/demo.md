# Presentation and demo runbook

## One-command demo data

```bash
cd backend
./scripts/demo.sh
```

This **only populates the configured database**: it applies migrations, ingests
the bundled MODIS/VIIRS samples, runs matching at 1.5 km / 30 min, and imports
**clearly labelled placeholder predictions** (`demo-placeholder-not-ml`). It
does not start or stop the API, and it does not delete an existing database.

Then start the binary yourself:

```bash
./bin/firms serve
```

The API address comes from `API_ADDR` in `.env` or the environment (default
`:9999`). If you already built the binary and do not want the script to rebuild
it, use `SKIP_BUILD=1 ./scripts/demo.sh`. In another terminal:

```bash
cd ../frontend
NEXT_PUBLIC_API_URL=http://localhost:9999 pnpm dev --port 3100
```

Open <http://localhost:3100/map> and select **Backend · month → January 2024**.
Kenya is the default area.

> The demo predictions are not ML results. They only prove that the import and
> display path works. Say this explicitly if you show the probability or
> calibrated-FRP fields.

## Pre-demo checklist

1. `git pull` in both `backend/` and `frontend/`.
2. Backend runs:
   ```bash
   cd backend && go test ./... && go run ./cmd/firms help
   ```
3. Frontend builds:
   ```bash
   cd frontend && pnpm install && pnpm lint && pnpm build
   ```
4. Run `./scripts/demo.sh` once before presenting so the binary and database
   are warm.
5. Keep a second terminal free for API calls.
6. Backups if the network or backend fails:
   - Frontend **Live · last 24 h** works without a FIRMS key.
   - **Sample history** works fully offline.
   - The static Kenya archive works offline.

## Suggested 7-minute flow

### 0:00–0:45 — Problem

- MODIS and VIIRS see fire differently: 1 km vs 375 m, different confidence
  scales, different overpasses.
- Naively combining them double-counts, mixes confidence scales, and hides
  single-sensor detections.
- The project produces a harmonized view **without pretending the sensors are
  interchangeable**.

### 0:45–1:45 — Architecture

Show the diagram in the root README or say:

```text
FIRMS → Go ingestion → SQLite → matching → API → Next.js map
                                   └→ ML predictions import
```

- Go backend: ingestion, standardization, matching, API, exports.
- Frontend: map, globe, calendar, comparisons.
- ML: separate component behind a prediction contract.

### 1:45–3:00 — Live frontend

1. Open `/map`.
2. **Live · last 24 h**: real public FIRMS files, no API key.
3. Change the pair window 30 → 180 min; explain that the backend/server already
   computed links to the widest window.
4. Click a MODIS and a VIIRS detection; show the pair links and the original
   confidence/FRP values.
5. Point out: “A pair means possible correspondence, not the same fire.”

### 3:00–4:15 — Backend mode

1. Switch to **Backend · month**, January 2024.
2. Show detections, pair counts, and the selected-record panel.
3. Explain the pipeline:
   - deterministic observation UID makes ingestion idempotent;
   - raw values are preserved next to standardized values;
   - matching is one-directional VIIRS → MODIS to avoid chaining.
4. Show `curl http://localhost:9999/api/integration/statistics?...` live.
5. Optional: open `/calendar`; the chip should say **Backend archive** for the
   months the backend has data for, and fall back to the static archive
   elsewhere.

### 4:15–5:15 — ML handoff

1. Open the selected-record panel and point to `P(MODIS detects footprint)` and
   calibrated FRP.
2. Say clearly: **in this demo these are placeholders**, labelled
   `demo-placeholder-not-ml`.
3. Explain the two production paths:
   - remote harmonizer service;
   - batch `predictions.json` keyed by `cluster_uid`.
4. Show the import contract:

   ```bash
   go run ./cmd/firms import-predictions \
     --run-id 1 --file predictions.json --model lightgbm-...
   ```

5. Explain validation: unknown/duplicate cluster IDs, probability outside
   `[0,1]`, or negative calibrated FRP rejects the whole batch.

### 5:15–6:15 — Evidence and limits

- Tests: `go test ./...`, `pnpm lint`, `pnpm build`.
- Matching metrics: distance/time distributions and unmatched counts via
  `/api/comparisons`.
- Limitations:
  - no ground truth, so report internal consistency;
  - swath-edge and overpass-time confounders;
  - ML is exploratory on a short sample;
  - calendar archive is computed on demand and cached for 10 minutes.

### 6:15–7:00 — Close

- Delivered: working ingestion, storage, matching, API, map/globe/calendar,
  exports, ML import contract.
- Next: longer FIRMS backfill, calibration, materialized monthly aggregates,
  production ML artifact.

## Exact demo commands

Terminal 1:

```bash
cd backend
./scripts/demo.sh
./bin/firms serve
```

Terminal 2:

```bash
cd frontend
NEXT_PUBLIC_API_URL=http://localhost:9999 pnpm dev --port 3100
```

Useful API checks:

```bash
curl 'http://localhost:9999/health'
curl 'http://localhost:9999/api/integration/statistics?start=2024-01-01&end=2024-01-03&bbox=33.5,-5,42,5.5'
curl 'http://localhost:9999/api/integration/firms?start=2024-01-01&end=2024-01-03&bbox=33.5,-5,42,5.5'
curl 'http://localhost:9999/api/integration/harmonized-observations?start=2024-01-01&end=2024-01-03'
```

## Likely questions

**Is a matched pair the same fire?** No. It is a potentially corresponding
observation under a distance/time rule.

**Why not just merge the CSVs?** Different confidence scales, resolutions, and
overpasses; merging creates duplicate-looking counts and false equivalence.

**Why SQLite?** Requested for the backend phase; the schema and API are ready
for Postgres/PostGIS when concurrency or spatial indexing demands it.

**Where is the ML model?** The ML repository has training/evaluation but no
served artifact yet. The backend's remote and batch contracts are ready; the
demo predictions are placeholders.

**How do you know it works?** Unit/API tests, clean-clone build, and internal
consistency metrics. Not ground-truth accuracy.
