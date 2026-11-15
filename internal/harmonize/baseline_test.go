package harmonize

import (
	"testing"
	"time"

	"firms-harmonizer/internal/ptr"
)

func observation(id int64, sensor string, lat, lon float64, hour, minute int, frp float64) Observation {
	return Observation{
		ObservationID:  id,
		ObservationUID: sensor + "-" + time.Date(2024, 1, 1, hour, minute, 0, 0, time.UTC).Format("1504"),
		Sensor:         sensor,
		Satellite:      "Terra",
		Latitude:       lat,
		Longitude:      lon,
		AcquiredAt:     time.Date(2024, 1, 1, hour, minute, 0, 0, time.UTC),
		FRP:            ptr.To(frp),
		FRPUnits:       "MW",
	}
}

func TestBaselineMatchesNearbyPairs(t *testing.T) {
	observations := []Observation{
		observation(1, "MODIS", -1.2921, 36.8219, 10, 30, 12.5),
		observation(2, "MODIS", -1.3000, 36.8300, 10, 35, 8.0),
		observation(3, "MODIS", -1.4000, 36.9000, 10, 40, 3.0),
		observation(101, "VIIRS", -1.2930, 36.8225, 10, 32, 10.0),
		observation(102, "VIIRS", -1.3010, 36.8310, 10, 36, 6.0),
		observation(103, "VIIRS", -1.5000, 37.0000, 11, 0, 2.0),
	}
	result := BaselineHarmonize(observations, Params{MaxDistanceKm: 2, MaxTimeMinutes: 180}, "run-1")
	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}
	if len(result.Clusters) != 4 {
		t.Fatalf("expected 4 clusters, got %d", len(result.Clusters))
	}
	composition := result.Metrics["clusters_by_composition"].(map[string]int)
	if composition["mixed"] != 2 || composition["modis_only"] != 1 || composition["viirs_only"] != 1 {
		t.Fatalf("unexpected composition: %+v", composition)
	}
	for _, cluster := range result.Clusters {
		if cluster.MODISCount == 1 && cluster.VIIRSCount == 1 && cluster.ObservationCount != 2 {
			t.Fatalf("unexpected mixed cluster: %+v", cluster)
		}
	}
}

func TestBaselineRespectsTimeWindow(t *testing.T) {
	observations := []Observation{
		observation(1, "MODIS", -1.2921, 36.8219, 10, 30, 12.5),
		observation(101, "VIIRS", -1.2930, 36.8225, 13, 0, 10.0),
	}
	result := BaselineHarmonize(observations, Params{MaxDistanceKm: 2, MaxTimeMinutes: 60}, "run-1")
	if len(result.Matches) != 0 {
		t.Fatalf("expected no matches outside the time window")
	}
}

func TestNoMatchingWithOneSensor(t *testing.T) {
	observations := []Observation{
		observation(1, "MODIS", -1.29, 36.82, 10, 30, 12.5),
	}
	result := BaselineHarmonize(observations, Params{MaxDistanceKm: 2, MaxTimeMinutes: 180}, "run-1")
	if len(result.Clusters) != 1 || len(result.Matches) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
