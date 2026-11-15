FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/firms ./cmd/firms

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata su-exec && \
    adduser -D -u 10001 firms && \
    mkdir -p /app/data /app/exports && \
    chown -R firms:firms /app

WORKDIR /app
COPY --from=build /out/firms /usr/local/bin/firms
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV DATABASE_PATH=/app/data/firms.db \
    EXPORT_DIR=/app/exports \
    AUTO_MIGRATE=true \
    API_ADDR=:9999 \
    TZ=UTC

EXPOSE 9999

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:9999/health || exit 1

# Starts as root only to fix volume ownership, then drops to `firms`.
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["serve"]
