package harmonize

import "time"

// Observation is the input contract for the built-in baseline and external
// harmonization services.
type Observation struct {
	ObservationID     int64     `json:"observation_id"`
	ObservationUID    string    `json:"observation_uid"`
	Sensor            string    `json:"sensor"`
	Satellite         string    `json:"satellite"`
	Latitude          float64   `json:"latitude"`
	Longitude         float64   `json:"longitude"`
	AcquiredAt        time.Time `json:"acquired_at"`
	ConfidenceRaw     *string   `json:"confidence_raw,omitempty"`
	ConfidenceNumeric *float64  `json:"confidence_numeric,omitempty"`
	ConfidenceClass   *string   `json:"confidence_class,omitempty"`
	ConfidenceScheme  *string   `json:"confidence_scheme,omitempty"`
	FRP               *float64  `json:"frp,omitempty"`
	FRPUnits          string    `json:"frp_units"`
}

type Params struct {
	MaxDistanceKm  float64        `json:"max_distance_km"`
	MaxTimeMinutes float64        `json:"max_time_minutes"`
	Extra          map[string]any `json:"extra,omitempty"`
}

type Request struct {
	RunUID       string        `json:"run_uid"`
	Method       string        `json:"method"`
	Params       Params        `json:"params"`
	Observations []Observation `json:"observations"`
}

type Member struct {
	ObservationID int64  `json:"observation_id"`
	Role          string `json:"role"`
}

type Cluster struct {
	ClusterUID        string         `json:"cluster_uid"`
	Members           []Member       `json:"members"`
	CentroidLatitude  float64        `json:"centroid_latitude"`
	CentroidLongitude float64        `json:"centroid_longitude"`
	StartTime         time.Time      `json:"start_time"`
	EndTime           time.Time      `json:"end_time"`
	ObservationCount  int            `json:"observation_count"`
	MODISCount        int            `json:"modis_count"`
	VIIRSCount        int            `json:"viirs_count"`
	Satellites        []string       `json:"satellites"`
	TotalFRP          *float64       `json:"total_frp"`
	MeanFRP           *float64       `json:"mean_frp"`
	ConfidenceSummary map[string]int `json:"confidence_summary"`
	Uncertainty       map[string]any `json:"uncertainty"`
}

type Match struct {
	MODISObservationID int64   `json:"modis_observation_id"`
	VIIRSObservationID int64   `json:"viirs_observation_id"`
	DistanceKm         float64 `json:"distance_km"`
	TimeDeltaMinutes   float64 `json:"time_delta_minutes"`
}

type Response struct {
	Clusters []Cluster      `json:"clusters"`
	Matches  []Match        `json:"matches"`
	Metrics  map[string]any `json:"metrics"`
}
