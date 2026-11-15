# Data model and field semantics

The schema lives in `internal/db/migrations/001_init.sql`; domain structs live
in `internal/models/models.go`. SQLite is the only supported database for now.

## Tables

### `observations`

| Column | Meaning |
| --- | --- |
| `observation_uid` | Deterministic SHA-256 identity (see below). Unique. |
| `sensor` | `MODIS` or `VIIRS`. |
| `satellite` | Normalized platform (`Terra`, `Aqua`, `Suomi NPP`, `NOAA-20`, `NOAA-21`). |
| `satellite_raw` | Original FIRMS satellite value. |
| `source` | FIRMS source ID (`MODIS_SP`, `VIIRS_SNPP_SP`, ...). |
| `source_product` | Product family (`MCD14ML`, `VNP14IMG`, ...). |
| `source_version` | `version` field from the CSV. |
| `processing` | `SP` or `NRT`. |
| `latitude`, `longitude` | WGS84 decimal degrees. |
| `acquired_at` | UTC acquisition time, stored as Unix milliseconds. |
| `source_date`, `source_time` | Original date and zero-padded HHMM string. |
| `confidence_raw` | Exact source value (`85`, `c8`, `h`, `n`, `l`, ...). |
| `confidence_numeric` | Only set when the source value is numeric. |
| `confidence_class` | `low`, `nominal`, or `high` display grouping. |
| `confidence_scheme` | Interpretation used (`modis_numeric_0_100`, `modis_c1_c9`, `viirs_categorical`, ...). |
| `frp` | Fire Radiative Power in MW, as provided. |
| `frp_units` | Always `MW` for FIRMS products. |
| `brightness` | `brightness` (MODIS) or `bright_ti4` (VIIRS). |
| `brightness_aux` | `bright_t31` (MODIS) or `bright_ti5` (VIIRS). |
| `scan`, `track` | FIRMS scan/track values in km. |
| `daynight` | `D` or `N`. |
| `fire_type` | FIRMS `type` (0 vegetation fire, 1 volcano, 2 other, 3 offshore). |
| `raw` | Verbatim original CSV row as JSON. |
| `first_seen_at`, `last_seen_at` | First/last ingestion observation. |
| `first_ingestion_run_id`, `last_ingestion_run_id` | Traceability to ingestion runs. |

Indexes: `(sensor, acquired_at)`, `(latitude, longitude)`, `acquired_at`,
`observation_uid`, plus filterable columns.

### `ingestion_runs`

Request metadata (source, bbox, dates, product, processing) and counts
(`fetched`, `inserted`, `duplicate`, `rejected`). Up to 100 row errors are
stored as JSON; the full rejected count is always recorded.

### `harmonization_runs`

Method, parameters, filters, status, input/cluster/match counts, and metrics
(all JSON where structured).

### `harmonized_observations`

A cross-sensor cluster: centroid, start/end time, sensor counts, satellites,
FRP totals, confidence summary, uncertainty (`spatial_radius_km`,
`time_span_minutes`), and optional ML outputs:

- `p_modis_detect` — probability in `[0, 1]`;
- `frp_calibrated` — calibrated FRP in MW;
- `prediction_model` — model/version label.

A cluster is an **analytical grouping**, not proof that the members are the
same fire event.

### `harmonized_members`

Links a cluster to its observations (`role` = `modis` or `viirs`).

### `observation_matches`

Pairwise links produced by a run: MODIS ID, VIIRS ID, distance (km), time
delta (minutes). Unique per `(run_id, modis_id, viirs_id)`.

### `export_jobs`

Export request, filters, output path, row count, and status.

## Identity and idempotency

`observation_uid = sha256(sensor|satellite|acq_date|acq_time|lat(5dp)|lon(5dp))`.

This deliberately excludes FRP, confidence, source, and product version so
that:

- NRT and standard-processing copies of the same detection do not duplicate;
- reprocessed FIRMS values update metadata instead of creating a new row;
- distinct scan pixels at distinct coordinates remain distinct.

The trade-off: two records at the exact same time and rounded coordinate
collapse into one, even if their measured values differ.

## Confidence semantics

MODIS and VIIRS confidence are **not** equivalent scales. The backend:

1. preserves `confidence_raw`;
2. records the interpretation used;
3. only fills `confidence_numeric` for numeric source values;
4. derives a display class:
   - numeric: `<30` low, `30–79.99` nominal, `>=80` high;
   - MODIS `c1..c9`: `c1–c3` low, `c4–c7` nominal, `c8–c9` high;
   - VIIRS `l/n/h`: low/nominal/high.

Thresholds are a presentation convention, not calibrated probabilities.

## Time and coordinates

- `acquired_at` and all run/cluster timestamps are stored as Unix milliseconds
  (UTC) so range filters and indexes behave consistently.
- The API serializes them as RFC3339 UTC strings.
- Coordinates are stored as delivered. No reprojection or snapping is applied.
- The schema uses indexed lat/lon columns. SQLite has no native geography
  type; a PostGIS deployment would be a separate migration decision.
