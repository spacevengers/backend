# Harmonization: baseline method and ML integration contract

## What the backend guarantees

- Original observations remain unchanged and queryable.
- Sensor, satellite, source product, and original confidence are preserved.
- A harmonized cluster links back to every member observation.
- Matching decisions and parameters are stored with the run.
- A cluster is a **potentially corresponding group**, never a confirmed
  duplicate or ground-truth fire event.

## Baseline algorithm (`method=baseline`)

Implemented in `internal/harmonize/baseline.go`.

1. Every MODIS detection opens a cluster.
2. Each VIIRS detection is assigned to the nearest MODIS detection within both
   thresholds:
   - `max_distance_km` (default **2.0 km**);
   - `max_time_minutes` (default **180 min**).
3. One MODIS detection may receive multiple VIIRS detections (expected because
   VIIRS is finer-grained).
4. Unmatched VIIRS detections become singleton clusters.
5. Every cluster receives:
   - mean centroid;
   - start/end time;
   - sensor/satellite counts;
   - FRP total and mean;
   - confidence-class counts;
   - `spatial_radius_km` (max member distance from centroid);
   - `time_span_minutes`.

Matching is one-directional (VIIRS → MODIS), avoiding chain effects where a
sequence of pairwise links merges unrelated fires. The cost is that a MODIS
detection can absorb several VIIRS detections; member lists and counts make
this visible.

### Why these defaults?

They are **starting values**, not validated optima:

- 2 km is roughly two MODIS 1 km pixels and several VIIRS 375 m pixels.
- 180 min spans the typical Terra (~10:30) to Aqua/VIIRS (~13:30) overpass
  difference in East Africa.

Evaluate a threshold sweep: 1/2/5 km and 30/60/180/360 min. Report how match
count, distance, time delta, and FRP agreement change. Do not present one
setting as universally correct.

### Complexity

Grid-based candidate lookup, roughly `O(n + m·k)` for `n` MODIS and `m` VIIRS
observations with a small candidate count `k`. The API guard is 250,000 input
observations per run.

## External harmonizer (`method=remote`)

Set `HARMONIZER_METHOD=remote` and
`HARMONIZER_ENDPOINT=http://ml:9000/harmonize`. The backend POSTs the contract
below and validates the response before persisting it. A non-2xx response or
contract violation marks the run failed with the error preserved.

### Request

```json
{
  "run_uid": "0e6f...",
  "method": "remote",
  "params": {
    "max_distance_km": 2.0,
    "max_time_minutes": 180.0,
    "extra": {}
  },
  "observations": [
    {
      "observation_id": 1,
      "observation_uid": "abc...",
      "sensor": "MODIS",
      "satellite": "Terra",
      "latitude": -1.2921,
      "longitude": 36.8219,
      "acquired_at": "2024-01-01T10:30:00Z",
      "confidence_raw": "85",
      "confidence_numeric": 85.0,
      "confidence_class": "high",
      "confidence_scheme": "modis_numeric_0_100",
      "frp": 12.5,
      "frp_units": "MW"
    }
  ]
}
```

### Response

```json
{
  "clusters": [
    {
      "cluster_uid": "stable-or-run-scoped-id",
      "members": [
        {"observation_id": 1, "role": "modis"},
        {"observation_id": 101, "role": "viirs"}
      ],
      "centroid_latitude": -1.2925,
      "centroid_longitude": 36.8222,
      "start_time": "2024-01-01T10:30:00Z",
      "end_time": "2024-01-01T10:32:00Z",
      "observation_count": 2,
      "modis_count": 1,
      "viirs_count": 1,
      "satellites": ["Suomi NPP", "Terra"],
      "total_frp": 22.5,
      "mean_frp": 11.25,
      "confidence_summary": {"high": 2},
      "uncertainty": {"spatial_radius_km": 0.06, "time_span_minutes": 2.0},
      "p_modis_detect": 0.73,
      "frp_calibrated": 11.25,
      "prediction_model": "lightgbm-2026-11-15"
    }
  ],
  "matches": [
    {
      "modis_observation_id": 1,
      "viirs_observation_id": 101,
      "distance_km": 0.12,
      "time_delta_minutes": 2.0
    }
  ],
  "metrics": {
    "method": "your-method",
    "notes": "free-form JSON metrics"
  }
}
```

Validation rules:

- every input `observation_id` appears in exactly one cluster;
- `role` matches the observation's sensor;
- `observation_count`, `modis_count`, and `viirs_count` match the member list;
- cluster UIDs are unique;
- matches reference input observations with the correct sensor roles;
- `p_modis_detect`, when present, is in `[0, 1]`;
- `frp_calibrated`, when present, is non-negative;
- JSON is UTF-8 and fits in memory (no streaming yet).

Predictions may also be imported after a run with
`POST /api/harmonization-runs/{id}/predictions` (or
`firms import-predictions`). That path is useful when the ML component runs as
a batch job rather than an HTTP service. See `docs/integration.md`.

### Implementing the ML service

Any HTTP service that accepts/returns this JSON works. Keep model artifacts
and training code outside the backend; return only decisions and metrics.

The contract currently excludes brightness, scan/track, day/night, and fire
type. If the method needs them, extend `Observation` in
`internal/harmonize/contract.go`, `toContract`, and the ML service together.
This is an intentional versioned interface change.

## Evaluation guidance

Internal-consistency metrics (returned by `/api/comparisons`):

- matched/unmatched counts by sensor and satellite pair;
- spatial displacement distribution;
- acquisition-time difference distribution;
- VIIRS/MODIS FRP ratio and difference before calibration;
- sensitivity to thresholds.

These are not accuracy metrics. Without independent reference data:

- describe results as internal consistency;
- manually inspect matched and unmatched samples;
- avoid claiming improved real-world detection accuracy;
- report assumptions and failure modes.

## Reproducibility

A run stores method, parameters, filters, input count, clusters, matches,
metrics, timestamps, and status. To reproduce, rerun the same method and
parameters against the same observation range, or export the observations and
run the ML code independently.
