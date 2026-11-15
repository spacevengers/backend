.PHONY: build run test vet fmt tidy migrate seed-sample docker-up docker-down

build:
	go build -o bin/firms ./cmd/firms

run:
	go run ./cmd/firms serve

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

migrate:
	go run ./cmd/firms init-db

seed-sample:
	go run ./cmd/firms ingest-file --source MODIS_SP --file testdata/modis_sample.csv
	go run ./cmd/firms ingest-file --source VIIRS_SNPP_SP --file testdata/viirs_sample.csv
	go run ./cmd/firms harmonize --start 2024-01-01 --end 2024-01-03

docker-up:
	docker compose up --build

docker-down:
	docker compose down
