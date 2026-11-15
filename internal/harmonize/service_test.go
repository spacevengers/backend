package harmonize

import (
	"testing"
	"time"
)

func validInput() []Observation {
	return []Observation{
		{ObservationID: 1, ObservationUID: "m1", Sensor: "MODIS", Satellite: "Terra",
			Latitude: -1.29, Longitude: 36.82, AcquiredAt: time.Now().UTC()},
		{ObservationID: 2, ObservationUID: "v1", Sensor: "VIIRS", Satellite: "Suomi NPP",
			Latitude: -1.29, Longitude: 36.82, AcquiredAt: time.Now().UTC()},
	}
}

func TestValidateResponseAcceptsCompleteOutput(t *testing.T) {
	response := Response{
		Clusters: []Cluster{{
			ClusterUID:       "c1",
			ObservationCount: 2,
			MODISCount:       1,
			VIIRSCount:       1,
			Members: []Member{
				{ObservationID: 1, Role: "modis"},
				{ObservationID: 2, Role: "viirs"},
			},
		}},
		Matches: []Match{{MODISObservationID: 1, VIIRSObservationID: 2}},
	}
	if err := validateResponse(response, validInput()); err != nil {
		t.Fatalf("expected valid response: %v", err)
	}
}

func TestValidateResponseRejectsMissingObservation(t *testing.T) {
	response := Response{
		Clusters: []Cluster{{
			ClusterUID: "c1",
			Members:    []Member{{ObservationID: 1, Role: "modis"}},
		}},
	}
	if err := validateResponse(response, validInput()); err == nil {
		t.Fatal("expected coverage error")
	}
}

func TestValidateResponseRejectsDuplicateMembership(t *testing.T) {
	response := Response{
		Clusters: []Cluster{
			{ClusterUID: "c1", Members: []Member{{ObservationID: 1, Role: "modis"}}},
			{ClusterUID: "c2", Members: []Member{
				{ObservationID: 1, Role: "modis"},
				{ObservationID: 2, Role: "viirs"},
			}},
		},
	}
	if err := validateResponse(response, validInput()); err == nil {
		t.Fatal("expected duplicate membership error")
	}
}

func TestValidateResponseRejectsWrongRole(t *testing.T) {
	response := Response{
		Clusters: []Cluster{{
			ClusterUID: "c1",
			Members: []Member{
				{ObservationID: 1, Role: "viirs"},
				{ObservationID: 2, Role: "viirs"},
			},
		}},
	}
	if err := validateResponse(response, validInput()); err == nil {
		t.Fatal("expected role error")
	}
}
