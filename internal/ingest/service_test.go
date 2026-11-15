package ingest

import (
	"context"
	"os"
	"testing"
	"time"

	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/testutil"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestIngestionIsIdempotent(t *testing.T) {
	repository, cleanup, err := testutil.OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	ctx := context.Background()
	product, _ := firms.GetProduct("MODIS_SP")
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	run, err := CreateRun(ctx, repository, product, "33.5,-5,42,5.5", start, end, "test")
	if err != nil {
		t.Fatal(err)
	}
	text := fixture(t, "modis_sample.csv")

	first, err := IngestText(ctx, repository, run, product, text)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IngestText(ctx, repository, run, product, text)
	if err != nil {
		t.Fatal(err)
	}
	if first.Inserted != 3 || first.Duplicates != 0 {
		t.Fatalf("unexpected first ingest: %+v", first)
	}
	if second.Inserted != 0 || second.Duplicates != 3 {
		t.Fatalf("unexpected second ingest: %+v", second)
	}
	total, err := repository.CountObservations(ctx, query.ObservationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected 3 stored observations, got %d", total)
	}
	if run.InsertedCount != 3 || run.DuplicateCount != 3 {
		t.Fatalf("run counters wrong: %+v", run)
	}
}
