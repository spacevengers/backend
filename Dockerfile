FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/firms ./cmd/firms

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 firms && \
    mkdir -p /app/data /app/exports && \
    chown -R firms:firms /app

WORKDIR /app
COPY --from=build /out/firms /usr/local/bin/firms

USER firms
ENV DATABASE_PATH=/app/data/firms.db \
    EXPORT_DIR=/app/exports \
    AUTO_MIGRATE=true \
    API_ADDR=:8000

EXPOSE 8000
ENTRYPOINT ["firms"]
CMD ["serve"]
