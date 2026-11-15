# Integration guide

Three components meet here:

```text
frontend (Next.js) ──> /api/integration/* ──> Go backend ──> SQLite
                                                ▲
                                                │ POST predictions
                                        ML batch job / service
```

## Frontend

Set:

```bash
# frontend/.env.local
NEXT_PUBLIC_API_URL=http://localhost:8000
```

The frontend has a **Backend · month** mode on the map and globe when the
variable is set. It requests one month at a time for the selected area (Kenya
by default) and reuses the existing live-map renderer. The calendar also uses
the backend archive summary when an area is selected.

### Adapter endpoints

These return the shapes in `frontend/src/lib/types.ts` and
`frontend/src/lib/firms/types.ts`:

| Endpoint | Returns |
| --- | --- |
| `GET /api/integration/observations` | `Observation[]` |
| `GET /api/integration/harmonized-observations` | `HarmonizedObservation[]` |
| `GET /api/integration/statistics` | `{counts, harmonized}` |
| `GET /api/integration/firms` | compact `LiveFirms` payload (modis/viirs arrays + links + cluster predictions) |
| `GET /api/integration/archive/summary` | monthly calendar summaries (`ArchiveSummary`) |
| `GET /api/integration/archive/{month}` | compact month payload (`ArchiveMonth`) |

Common query parameters: `start`, `end`, `bbox`, repeated `sensor`, and
optionally `run_id`. `firms` also accepts:

- `max_distance_km` (default `1.5`)
- `pair_minutes` (default `30`) — the rule shown in the UI
- `max_minutes` (default `180`) — links are computed to this widest window,
  so the UI can narrow to 30/60/120/180 without refetching

The native API (`/api/observations`, `/api/harmonized-observations`, ...) is
unchanged and remains the contract for exports and non-frontend clients.

## ML

The backend stores two optional prediction fields per harmonized cluster:

- `p_modis_detect` — P(MODIS detects this footprint), in `[0, 1]`
- `frp_calibrated` — calibrated FRP, MW, kept separate from source FRP

### Option A: remote harmonizer

Set `HARMONIZER_METHOD=remote` and `HARMONIZER_ENDPOINT`. The service receives
the contract in `docs/harmonization.md` and may set `p_modis_detect`,
`frp_calibrated`, and `prediction_model` on each cluster.

### Option B: batch prediction import

1. Run baseline (or remote) harmonization to create clusters.
2. Export observations / cluster members from the native API.
3. Produce predictions keyed by `cluster_uid`.
4. Import:

```bash
curl -X POST http://localhost:8000/api/harmonization-runs/1/predictions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "lightgbm-2026-11-15",
    "predictions": [
      {"cluster_uid": "abc...", "p_modis_detect": 0.73, "frp_calibrated": 11.25}
    ]
  }'
```

or:

```bash
go run ./cmd/firms import-predictions \
  --run-id 1 --file predictions.json --model lightgbm-2026-11-15
```

The file may be `{"model": "...", "predictions": [...]}` or a bare
`[...]` array. Imports are validated all-or-nothing: unknown cluster UIDs,
duplicate UIDs, probabilities outside `[0, 1]`, or negative FRP reject the
whole batch.

### What ML can read today

- `GET /api/observations` — full source fields, including brightness, scan,
  track, day/night, confidence, and raw rows.
- `GET /api/harmonized-observations?run_id=...&include_members=true` — clusters,
  member IDs, and member observations.
- `GET /api/comparisons?run_id=...` — internal consistency metrics.

The frontend displays `p_modis_detect` and `frp_calibrated` when present and
shows “not produced” otherwise.

## Recommended end-to-end flow

```text
1. firms ingest --source MODIS_SP ...
2. firms ingest --source VIIRS_SNPP_SP ...
3. firms harmonize --start ... --end ... --max-distance 1.5 --max-time 30
4. ML: read observations/clusters -> train or predict -> predictions.json
5. firms import-predictions --run-id N --file predictions.json --model ...
6. frontend: switch to Backend · month
```

## Known gaps

- The calendar falls back to the frontend's static Kenya archive when the
  backend is unreachable.
- ML serving is not implemented in the ML repository yet; the batch import and
  remote HTTP contract are ready.
- The frontend's Backend mode requests at most one month and 100,000
  observations per call. Use the native paginated API for larger extracts.
- Archive summaries are cached for 10 minutes and capped at 120 months.
