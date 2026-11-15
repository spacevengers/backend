# FIRMS Harmonizer Backend (Go)

Go service for ingesting NASA FIRMS MODIS and VIIRS active-fire
observations, standardizing them, running spatial-temporal harmonization, and
serving analysis-ready data to the frontend.

**Status:** early scaffold. The baseline matcher is a transparent reference
method, not a validated scientific conclusion. The ML harmonization method is
owned by a separate component.

## Quick start

```bash
cd backend
cp .env.example .env          # add FIRMS_MAP_KEY for real downloads
go run ./cmd/firms init-db

# Load the bundled sample fixtures (no FIRMS key needed)
go run ./cmd/firms ingest-file --source MODIS_SP --file testdata/modis_sample.csv
go run ./cmd/firms ingest-file --source VIIRS_SNPP_SP --file testdata/viirs_sample.csv
go run ./cmd/firms harmonize --start 2024-01-01 --end 2024-01-03

go run ./cmd/firms serve
```

- API docs: <http://localhost:8000/docs> is not generated; see
  [`docs/api.md`](docs/api.md) and `/health`.
- Tests: `go test ./...`
- Docker: `docker compose up --build`

## Stack

| Layer | Choice |
| --- | --- |
| HTTP | Go standard library `net/http` (`ServeMux` method patterns) |
| Database | SQLite (`modernc.org/sqlite`, pure Go) |
| Migrations | Embedded SQL, applied by `firms init-db` / `AUTO_MIGRATE` |
| Ingestion | FIRMS Area API client with date chunking and retries |
| Harmonization | Built-in baseline + HTTP contract for an external ML service |
| Exports | Streaming CSV and GeoJSON files |
| Deployment | Multi-stage Docker image + Compose volume |

## Layout

```text
backend/
├── cmd/firms/               CLI entrypoint (serve, init-db, ingest, harmonize)
├── internal/
│   ├── api/                 HTTP handlers and request parsing
│   ├── config/              env/.env configuration
│   ├── db/                  SQLite connection + embedded migrations
│   ├── export/              CSV/GeoJSON generation
│   ├── firms/               FIRMS products, client, CSV standardization
│   ├── harmonize/           contract, baseline matcher, persistence
│   ├── ingest/              run creation and idempotent ingestion
│   ├── models/              domain structs
│   ├── query/               shared SQL filters
│   ├── store/               database access
│   ├── testutil/            temporary test database helper
│   └── timeutil/            UTC/epoch helpers
├── testdata/                sample MODIS/VIIRS CSVs
├── docs/                    API, data model, method, decisions
├── Dockerfile
└── docker-compose.yml
```

## CLI

```bash
go run ./cmd/firms serve                       # start API
go run ./cmd/firms init-db                     # apply migrations
go run ./cmd/firms ingest \
  --source MODIS_SP --start 2024-01-01 --end 2024-01-05
go run ./cmd/firms ingest-file \
  --source VIIRS_SNPP_SP --file sample.csv
go run ./cmd/firms harmonize \
  --start 2024-01-01 --end 2024-01-31 --max-distance 2 --max-time 180
go run ./cmd/firms import-predictions \
  --run-id 1 --file predictions.json --model lightgbm-2026-11-15
go run ./cmd/firms stats --start 2024-01-01 --end 2024-01-31
```

## Integration

- The frontend calls `/api/integration/*` view adapters for its map, globe, and
  statistics views.
- ML can return predictions through the remote harmonizer contract or import
  batch predictions with `firms import-predictions`.
- See [`docs/integration.md`](docs/integration.md) for the full handoff.

## Scientific guardrails

- MODIS and VIIRS confidence values are not treated as equivalent.
- FRP is stored as delivered; no calibration is applied by default.
- Matched pairs are labelled potentially corresponding observations, not
  confirmed duplicates.
- Harmonized clusters are analytical groups, not ground-truth fire events.
- Internal consistency metrics are distinguished from accuracy.

See [`docs/decisions.md`](docs/decisions.md) for assumptions and open team
decisions.

## Relationship to the Python archive

`archive/python-backend/` contains the earlier FastAPI prototype. It is kept
for reference only; the running backend is this Go service.
