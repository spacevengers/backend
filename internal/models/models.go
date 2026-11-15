package models

import (
	"database/sql/driver"

	"fmt"
	"time"
)

// JSONText stores JSON in a TEXT column and serializes it as raw JSON.
type JSONText []byte

func (j *JSONText) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], value...)
	case string:
		*j = append((*j)[:0], []byte(value)...)
	default:
		return fmt.Errorf("cannot scan %T into JSONText", src)
	}
	return nil
}

func (j JSONText) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "null", nil
	}
	return string(j), nil
}

func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

type IngestionRun struct {
	ID                 int64      `json:"id"`
	RunUID             string     `json:"run_uid"`
	Status             string     `json:"status"`
	Trigger            string     `json:"trigger"`
	Source             string     `json:"source"`
	Sensor             string     `json:"sensor"`
	Product            *string    `json:"product"`
	Processing         *string    `json:"processing"`
	Area               string     `json:"area"`
	StartDate          string     `json:"start_date"`
	EndDate            string     `json:"end_date"`
	RequestFingerprint string     `json:"-"`
	RequestedAt        time.Time  `json:"requested_at"`
	StartedAt          *time.Time `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	FetchedCount       int        `json:"fetched_count"`
	InsertedCount      int        `json:"inserted_count"`
	DuplicateCount     int        `json:"duplicate_count"`
	RejectedCount      int        `json:"rejected_count"`
	Error              *string    `json:"error"`
	Errors             JSONText   `json:"errors,omitempty"`
}

type Observation struct {
	ID                int64     `json:"id"`
	ObservationUID    string    `json:"observation_uid"`
	Sensor            string    `json:"sensor"`
	Satellite         string    `json:"satellite"`
	SatelliteRaw      *string   `json:"satellite_raw"`
	Source            string    `json:"source"`
	SourceProduct     string    `json:"source_product"`
	SourceVersion     *string   `json:"source_version"`
	Processing        *string   `json:"processing"`
	Latitude          float64   `json:"latitude"`
	Longitude         float64   `json:"longitude"`
	AcquiredAt        time.Time `json:"acquired_at"`
	SourceDate        string    `json:"source_date"`
	SourceTime        string    `json:"source_time"`
	ConfidenceRaw     *string   `json:"confidence_raw"`
	ConfidenceNumeric *float64  `json:"confidence_numeric"`
	ConfidenceClass   *string   `json:"confidence_class"`
	ConfidenceScheme  *string   `json:"confidence_scheme"`
	FRP               *float64  `json:"frp"`
	FRPUnits          string    `json:"frp_units"`
	Brightness        *float64  `json:"brightness"`
	BrightnessAux     *float64  `json:"brightness_aux"`
	Scan              *float64  `json:"scan"`
	Track             *float64  `json:"track"`
	DayNight          *string   `json:"daynight"`
	FireType          *int      `json:"fire_type"`
	Raw               JSONText  `json:"raw,omitempty"`
	FirstSeenAt       time.Time `json:"first_seen_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	IngestedAt        time.Time `json:"ingested_at"`
}

type HarmonizationRun struct {
	ID           int64      `json:"id"`
	RunUID       string     `json:"run_uid"`
	Method       string     `json:"method"`
	Status       string     `json:"status"`
	Params       JSONText   `json:"params"`
	Filters      JSONText   `json:"filters"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	InputCount   int        `json:"input_count"`
	ClusterCount int        `json:"cluster_count"`
	MatchCount   int        `json:"match_count"`
	Metrics      JSONText   `json:"metrics"`
	Error        *string    `json:"error"`
	CreatedAt    time.Time  `json:"created_at"`
}

type HarmonizedObservation struct {
	ID                int64              `json:"id"`
	ClusterUID        string             `json:"cluster_uid"`
	RunID             int64              `json:"run_id"`
	CentroidLatitude  float64            `json:"centroid_latitude"`
	CentroidLongitude float64            `json:"centroid_longitude"`
	StartTime         time.Time          `json:"start_time"`
	EndTime           time.Time          `json:"end_time"`
	ObservationCount  int                `json:"observation_count"`
	MODISCount        int                `json:"modis_count"`
	VIIRSCount        int                `json:"viirs_count"`
	Satellites        JSONText           `json:"satellites"`
	TotalFRP          *float64           `json:"total_frp"`
	MeanFRP           *float64           `json:"mean_frp"`
	ConfidenceSummary JSONText           `json:"confidence_summary"`
	Uncertainty       JSONText           `json:"uncertainty"`
	PModisDetect      *float64           `json:"p_modis_detect"`
	FRPCalibrated     *float64           `json:"frp_calibrated"`
	PredictionModel   *string            `json:"prediction_model"`
	CreatedAt         time.Time          `json:"created_at"`
	Members           []HarmonizedMember `json:"members"`
}

type HarmonizedMember struct {
	ID            int64        `json:"-"`
	HarmonizedID  int64        `json:"-"`
	ObservationID int64        `json:"observation_id"`
	Role          string       `json:"role"`
	Observation   *Observation `json:"observation,omitempty"`
}

type ObservationMatch struct {
	ID                 int64     `json:"id"`
	RunID              int64     `json:"run_id"`
	MODISObservationID int64     `json:"modis_observation_id"`
	VIIRSObservationID int64     `json:"viirs_observation_id"`
	DistanceKm         float64   `json:"distance_km"`
	TimeDeltaMinutes   float64   `json:"time_delta_minutes"`
	Method             string    `json:"method"`
	CreatedAt          time.Time `json:"created_at"`

	MODIS *Observation `json:"-"`
	VIIRS *Observation `json:"-"`
}

type ExportJob struct {
	ID          int64      `json:"id"`
	ExportUID   string     `json:"export_uid"`
	Status      string     `json:"status"`
	Dataset     string     `json:"dataset"`
	Format      string     `json:"format"`
	Filters     JSONText   `json:"filters"`
	FilePath    *string    `json:"-"`
	RowCount    int        `json:"row_count"`
	Error       *string    `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}
