package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decodeArray(t *testing.T, payload []byte) []map[string]any {
	t.Helper()
	var decoded []map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode array: %v: %s", err, payload)
	}
	return decoded
}

func TestIntegrationEndpointsAndPredictionImport(t *testing.T) {
	server, repository := newTestServer(t)
	seed(t, repository)

	status, payload := requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/harmonization-runs", map[string]any{
			"start": "2024-01-01T00:00:00Z",
			"end":   "2024-01-03T23:59:59Z",
			"bbox":  "33.5,-5,42,5.5",
		})
	if status != http.StatusAccepted {
		t.Fatalf("harmonization status %d: %s", status, payload)
	}
	runID := decodeMap(t, payload)["id"].(float64)

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/integration/observations?start=2024-01-01&end=2024-01-03", nil)
	if status != http.StatusOK {
		t.Fatalf("integration observations status %d: %s", status, payload)
	}
	observations := decodeArray(t, payload)
	if len(observations) != 6 {
		t.Fatalf("expected 6 observations, got %d", len(observations))
	}
	if observations[0]["observation_id"] == "" || observations[0]["sensor"] == "" {
		t.Fatalf("missing observation fields: %+v", observations[0])
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/integration/harmonized-observations?run_id="+jsonNumber(runID), nil)
	if status != http.StatusOK {
		t.Fatalf("integration harmonized status %d: %s", status, payload)
	}
	clusters := decodeArray(t, payload)
	if len(clusters) != 4 {
		t.Fatalf("expected 4 clusters, got %d", len(clusters))
	}
	var mixed map[string]any
	for _, cluster := range clusters {
		if cluster["match_status"] == "matched" {
			mixed = cluster
			break
		}
	}
	if mixed == nil {
		t.Fatal("expected a matched cluster")
	}
	if len(mixed["member_ids"].([]any)) != 2 {
		t.Fatalf("expected two member ids: %+v", mixed)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/harmonization-runs/"+jsonNumber(runID)+"/predictions",
		map[string]any{
			"model": "test-model",
			"predictions": []map[string]any{{
				"cluster_uid":    mixed["cluster_uid"],
				"p_modis_detect": 0.73,
				"frp_calibrated": 11.25,
			}},
		})
	if status != http.StatusOK {
		t.Fatalf("prediction import status %d: %s", status, payload)
	}
	if decodeMap(t, payload)["updated"].(float64) != 1 {
		t.Fatalf("expected one updated prediction")
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/harmonized-observations/"+mixed["harmonized_id"].(string), nil)
	if status != http.StatusOK {
		t.Fatalf("cluster detail status %d: %s", status, payload)
	}
	detail := decodeMap(t, payload)
	if detail["p_modis_detect"].(float64) != 0.73 || detail["prediction_model"] != "test-model" {
		t.Fatalf("prediction not stored: %+v", detail)
	}

	status, _ = requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/harmonization-runs/"+jsonNumber(runID)+"/predictions",
		map[string]any{
			"model": "bad-model",
			"predictions": []map[string]any{{
				"cluster_uid":    mixed["cluster_uid"],
				"p_modis_detect": 1.5,
			}},
		})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid probability to be rejected, got %d", status)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/integration/statistics?start=2024-01-01&end=2024-01-03", nil)
	if status != http.StatusOK {
		t.Fatalf("integration statistics status %d: %s", status, payload)
	}
	stats := decodeMap(t, payload)
	counts := stats["counts"].(map[string]any)
	if counts["MODIS"].(float64) != 3 || counts["VIIRS"].(float64) != 3 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
	harmonized := stats["harmonized"].(map[string]any)
	if harmonized["matched"].(float64) != 2 ||
		harmonized["modis_only"].(float64) != 1 ||
		harmonized["viirs_only"].(float64) != 1 {
		t.Fatalf("unexpected harmonized counts: %+v", harmonized)
	}

	status, payload = requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/integration/firms?start=2024-01-01&end=2024-01-03", nil)
	if status != http.StatusOK {
		t.Fatalf("integration firms status %d: %s", status, payload)
	}
	firmsPayload := decodeMap(t, payload)
	if len(firmsPayload["modis"].([]any)) != 3 || len(firmsPayload["viirs"].([]any)) != 3 {
		t.Fatalf("unexpected firms arrays")
	}
	if len(firmsPayload["links"].([]any)) != 2 {
		t.Fatalf("expected 2 links, got %d", len(firmsPayload["links"].([]any)))
	}
	rule := firmsPayload["rule"].(map[string]any)
	if rule["maxKm"].(float64) != 1.5 || rule["widestMinutes"].(float64) != 180 {
		t.Fatalf("unexpected rule: %+v", rule)
	}
}
