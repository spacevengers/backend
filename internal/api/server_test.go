package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"firms-harmonizer/internal/config"
	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/ingest"
	"firms-harmonizer/internal/store"
	"firms-harmonizer/internal/testutil"
)

func seed(t *testing.T, repository *store.Store) {
	t.Helper()
	ctx := context.Background()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	for source, file := range map[string]string{
		"MODIS_SP":      "modis_sample.csv",
		"VIIRS_SNPP_SP": "viirs_sample.csv",
	} {
		product, _ := firms.GetProduct(source)
		data, err := os.ReadFile("../../testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		run, err := ingest.CreateRun(ctx, repository, product, "33.5,-5,42,5.5", start, end, "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.IngestText(ctx, repository, run, product, string(data)); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	repository, cleanup, err := testutil.OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	cfg := config.Config{
		ExportDir:        t.TempDir(),
		MaxExportRows:    10000,
		SchemaVersion:    "test",
		HarmonizerMethod: "baseline",
		HarmonizeMaxKm:   2,
		HarmonizeMaxMin:  180,
		SynchronousJobs:  true,
		DefaultBBox:      "33.5,-5,42,5.5",
	}
	server := httptest.NewServer(New(cfg, repository).Handler())
	t.Cleanup(server.Close)
	return server, repository
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func decodeMap(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode response: %v: %s", err, payload)
	}
	return decoded
}

func TestObservationListAndFilters(t *testing.T) {
	server, repository := newTestServer(t)
	seed(t, repository)

	status, payload := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/observations", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	listing := decodeMap(t, payload)
	if listing["total"].(float64) != 6 {
		t.Fatalf("expected 6 observations, got %v", listing["total"])
	}
	items := listing["items"].([]any)
	if _, exists := items[0].(map[string]any)["raw"]; exists {
		t.Fatalf("list response should not include raw rows")
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/observations?sensor=VIIRS", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	if decodeMap(t, payload)["total"].(float64) != 3 {
		t.Fatalf("expected 3 VIIRS observations")
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/observations?confidence_class=low", nil)
	if status != http.StatusOK || decodeMap(t, payload)["total"].(float64) != 2 {
		t.Fatalf("unexpected low-confidence filter response: %s", payload)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/observations/geojson", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	geojson := decodeMap(t, payload)
	if geojson["type"] != "FeatureCollection" || len(geojson["features"].([]any)) != 6 {
		t.Fatalf("unexpected geojson response")
	}
}

func TestStatisticsAndMetadata(t *testing.T) {
	server, repository := newTestServer(t)
	seed(t, repository)

	status, payload := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/statistics", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	stats := decodeMap(t, payload)
	totals := stats["totals"].(map[string]any)
	if totals["observations"].(float64) != 6 || totals["modis"].(float64) != 3 {
		t.Fatalf("unexpected totals: %+v", totals)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/metadata", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	metadata := decodeMap(t, payload)
	if len(metadata["available_sources"].([]any)) != 8 {
		t.Fatalf("expected 8 supported sources")
	}
}

func TestHarmonizationAndComparisons(t *testing.T) {
	server, repository := newTestServer(t)
	seed(t, repository)

	status, payload := requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/harmonization-runs", map[string]any{
			"start": "2024-01-01T00:00:00Z",
			"end":   "2024-01-03T23:59:59Z",
			"bbox":  "33.5,-5,42,5.5",
		})
	if status != http.StatusAccepted {
		t.Fatalf("status %d: %s", status, payload)
	}
	run := decodeMap(t, payload)
	runID := run["id"].(float64)

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/harmonization-runs/"+jsonNumber(runID), nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	run = decodeMap(t, payload)
	if run["status"] != "completed" || run["match_count"].(float64) != 2 {
		t.Fatalf("unexpected run: %+v", run)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/harmonized-observations?run_id="+jsonNumber(runID)+"&include_members=true", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	clusters := decodeMap(t, payload)
	if clusters["total"].(float64) != 4 {
		t.Fatalf("expected 4 clusters, got %v", clusters["total"])
	}
	memberTotal := 0
	for _, item := range clusters["items"].([]any) {
		memberTotal += len(item.(map[string]any)["members"].([]any))
	}
	if memberTotal != 6 {
		t.Fatalf("expected 6 cluster members, got %d", memberTotal)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/comparisons?run_id="+jsonNumber(runID), nil)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, payload)
	}
	comparisons := decodeMap(t, payload)
	if comparisons["available"] != true {
		t.Fatalf("comparisons not available")
	}
	matches := comparisons["matches"].(map[string]any)
	if matches["count"].(float64) != 2 {
		t.Fatalf("expected 2 matches")
	}
}

func TestExportCSV(t *testing.T) {
	server, repository := newTestServer(t)
	seed(t, repository)

	status, payload := requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/exports", map[string]any{
			"dataset": "observations",
			"format":  "csv",
		})
	if status != http.StatusCreated {
		t.Fatalf("status %d: %s", status, payload)
	}
	job := decodeMap(t, payload)
	if job["status"] != "completed" || job["row_count"].(float64) != 6 {
		t.Fatalf("unexpected export job: %+v", job)
	}
	downloadURL := server.URL + job["download_url"].(string)
	status, body := requestJSON(t, server.Client(), http.MethodGet, downloadURL, nil)
	if status != http.StatusOK {
		t.Fatalf("download status %d: %s", status, body)
	}
	if !bytes.Contains(body, []byte("observation_uid")) {
		t.Fatalf("CSV missing header: %s", body)
	}
}

func TestIngestionRequiresMapKey(t *testing.T) {
	server, _ := newTestServer(t)
	status, payload := requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/ingestion/runs", map[string]any{
			"source":     "MODIS_SP",
			"start_date": "2024-01-01",
			"end_date":   "2024-01-02",
		})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d: %s", status, payload)
	}
}

func jsonNumber(value float64) string {
	encoded, _ := json.Marshal(int64(value))
	return string(encoded)
}
