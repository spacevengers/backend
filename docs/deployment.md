# Podman deployment

## Why host networking

Some VPS/LXC hosts cannot create a Podman bridge:

```text
netavark: create bridge: Netlink error: Operation not supported (os error 95)
```

The compose file therefore uses `network_mode: host`. The API listens directly
on the host at `:9999`; there is no `ports:` mapping. Make sure nothing else is
using 9999.

If host networking is unavailable too, run the image directly:

```bash
podman build -t space-backend .

podman run -d --name space-backend --network host \
  --env-file .env \
  -e DATABASE_PATH=/app/data/firms.db \
  -e EXPORT_DIR=/app/exports \
  -e API_ADDR=:9999 \
  -v space-data:/app/data \
  -v space-exports:/app/exports \
  space-backend serve
```

## Run

```bash
cd backend
cp .env.example .env          # set FIRMS_MAP_KEY, ADMIN_TOKEN, CORS_ORIGINS
podman compose up -d --build
podman compose ps
podman logs -f space-backend
```

API:

```bash
curl http://localhost:9999/health
```

## Why the entrypoint exists

SQLite needs write access to the **database directory** for `-wal` and `-shm`
files. A named volume is often created root-owned, which caused:

```text
error: ping sqlite: attempt to write a readonly database (1544)
```

`docker-entrypoint.sh` starts as root, fixes ownership of `/app/data` and
`/app/exports`, then drops to the unprivileged `firms` user before starting the
service. Do not run the Go binary directly as a user that cannot write the DB
directory.

## Seed data in the container

The image does not include the sample CSVs. Copy them in and run as the app
user:

```bash
podman cp testdata/modis_sample.csv space-backend:/tmp/modis.csv
podman cp testdata/viirs_sample.csv space-backend:/tmp/viirs.csv

podman exec --user firms space-backend firms ingest-file \
  --source MODIS_SP --file /tmp/modis.csv
podman exec --user firms space-backend firms ingest-file \
  --source VIIRS_SNPP_SP --file /tmp/viirs.csv
podman exec --user firms space-backend firms harmonize \
  --start 2024-01-01 --end 2024-01-03 --max-distance 1.5 --max-time 30
podman exec --user firms space-backend firms demo-predictions --run-id 1
```

## Frontend on another host

Set the frontend build/runtime variable to the public backend URL:

```bash
NEXT_PUBLIC_API_URL=https://api.example.org
```

and allow that frontend origin on the backend:

```bash
CORS_ORIGINS=https://app.example.org
```

Both are read at startup/build time respectively; restart or rebuild after
changing them.

## Persistence and backup

```bash
podman volume ls
podman volume inspect backend_firms-data
podman volume inspect backend_firms-exports
```

Copy the SQLite database out for a backup:

```bash
podman cp space-backend:/app/data/firms.db ./firms-backup.db
```

Do not delete the volumes unless you intend to lose ingested data:

```bash
podman compose down          # keeps volumes
podman compose down -v       # DELETES data and exports
```

## If port 9999 is busy

```bash
ss -ltnp | grep 9999
```

Stop the old process, or change the published port in `docker-compose.yml` and
`API_ADDR` together.

## systemd note

If you go back to systemd instead of Podman, the service user must own the DB
directory and file. `chown -R root:web .` makes the DB unwritable for a service
running as another user. Use the actual service user and ensure any
`ReadWritePaths=` entry includes the database directory.
