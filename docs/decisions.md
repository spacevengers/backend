# Decisions, assumptions, and open questions

This is the backend's decision log. Update it as the team agrees on scope.

## Decided for the Go scaffold

| Topic | Decision | Reason |
| --- | --- | --- |
| Language | Go | Team preference; single static binary, simple deployment. |
| HTTP | Standard library `net/http` | No framework dependency; method-aware `ServeMux`. |
| Database | SQLite (`modernc.org/sqlite`) | Requested for now; pure Go, no CGO, easy local setup. |
| Migrations | Embedded SQL files + small runner | No external migration tool; applied on startup/CLI. |
| Timestamps | Unix milliseconds (UTC) in SQLite | Reliable range filters and indexes; RFC3339 at the API edge. |
| Observation identity | Hash of sensor, satellite, date, time, lat, lon | Idempotent ingestion across NRT/SP and reprocessing. |
| Raw retention | Full source row in `observations.raw` | Traceability without a second raw table. |
| Confidence | Preserve raw, record scheme, derive display class | Avoids asserting MODIS/VIIRS numeric equivalence. |
| FRP | Store as delivered, unit `MW`, no calibration | Calibration is a research decision, not an ingestion default. |
| Baseline matching | 2 km / 180 min, VIIRS→MODIS, configurable | Transparent starting point; requires sensitivity analysis. |
| ML integration | HTTP JSON contract, validated server-side | Keeps ML independent of storage/API internals. |
| Authentication | Optional shared `X-Admin-Token` | Adequate for a prototype; replace before public exposure. |
| Exports | Synchronous CSV/GeoJSON with row cap | Simple and auditable; move to a queue if exports grow. |
| Background work | In-process goroutines, persisted runs | Avoids a broker for the first version. |
| API docs | Markdown contract | The standard library server does not generate OpenAPI. |

## Assumptions to verify

1. **Product mapping.** `MCD14DL/MCD14ML`, `VNP14IMG`, `VJ114IMG`, and
   `VJ214IMG` are used as product families. Confirm against current FIRMS
   documentation and record exact collection/version strings in the report.
2. **FIRMS confidence fields.** MODIS is treated as numeric 0–100 by default,
   with `c1..c9` support. VIIRS is treated as categorical (`l/n/h`). Verify
   against the actual CSV files used.
3. **Acquisition time.** FIRMS `acq_time` is UTC HHMM. Verify for the chosen
   products and document exceptions.
4. **UID rounding.** Coordinates are rounded to 5 decimal places (~1 m). Two
   genuinely distinct detections at identical time/location will collapse.
5. **Matching direction.** One MODIS can absorb several VIIRS. This is
   intentional; alternative many-to-many methods belong to the ML work.
6. **No antimeridian support.** Bounding boxes require `west < east`.
7. **Export end dates.** Date-only `end` in query/body is expanded to the end
   of the UTC day.

## Open team decisions

| Decision | Recommendation | Owner/status |
| --- | --- | --- |
| Study area | Kenya bbox `33.5,-5.0,42.0,5.5` | To agree |
| Study period | One dry season + one wet season, not a full year at first | To agree |
| Products | MODIS SP + VIIRS SNPP/NOAA-20 SP; NRT only for a separate comparison | To agree |
| Primary research question | Cross-sensor consistency and matching sensitivity before calibration | To agree |
| Confidence handling | Report raw + class; never merge scales without justification | To agree |
| FRP calibration | Evaluate only after baseline matching is stable | ML |
| Ground truth | Find an independent reference or label results as internal consistency | Team |
| SQLite limits | Move to Postgres/PostGIS if concurrent writes or spatial queries become a bottleneck | Backend |
| Auth | Replace shared token with OIDC/API keys before public deployment | Backend |
| Background jobs | Add a queue if ingestion/exports exceed request lifetimes or need retries | Backend |
| Pagination | Offset is fine for the dashboard; keyset for very large scans | Backend |
| API docs | Generate OpenAPI from a spec or annotations if the frontend needs codegen | Backend |

## Explicit non-goals

- Treating every hotspot as a unique fire.
- Calling matched pairs confirmed duplicates.
- Claiming calibrated FRP without evaluation.
- Replacing official fire products or emergency response.
- Predicting future fire occurrence.
