package export

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
	"firms-harmonizer/internal/timeutil"
)

var observationColumns = []string{
	"id", "observation_uid", "sensor", "satellite", "source", "source_product",
	"source_version", "processing", "latitude", "longitude", "acquired_at",
	"source_date", "source_time", "confidence_raw", "confidence_numeric",
	"confidence_class", "confidence_scheme", "frp", "frp_units", "brightness",
	"brightness_aux", "scan", "track", "daynight", "fire_type", "first_seen_at",
	"last_seen_at",
}

var harmonizedColumns = []string{
	"id", "cluster_uid", "run_id", "centroid_latitude", "centroid_longitude",
	"start_time", "end_time", "observation_count", "modis_count", "viirs_count",
	"satellites", "total_frp", "mean_frp", "confidence_summary", "uncertainty",
	"p_modis_detect", "frp_calibrated", "prediction_model",
}

type Service struct {
	Store     *store.Store
	ExportDir string
	MaxRows   int
}

func (s *Service) Create(
	ctx context.Context,
	dataset, format string,
	observationFilter query.ObservationFilter,
	harmonizedFilter query.HarmonizedFilter,
) (*models.ExportJob, error) {
	if dataset != "observations" && dataset != "harmonized" {
		return nil, fmt.Errorf("dataset must be 'observations' or 'harmonized'")
	}
	if format != "csv" && format != "geojson" {
		return nil, fmt.Errorf("format must be 'csv' or 'geojson'")
	}
	if err := os.MkdirAll(s.ExportDir, 0o755); err != nil {
		return nil, err
	}

	filterPayload, _ := json.Marshal(map[string]any{
		"observations": observationFilter,
		"harmonized":   harmonizedFilter,
	})
	job := &models.ExportJob{
		ExportUID: uuid.NewString(),
		Status:    "running",
		Dataset:   dataset,
		Format:    format,
		Filters:   models.JSONText(filterPayload),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.CreateExport(ctx, job); err != nil {
		return nil, err
	}

	path := filepath.Join(s.ExportDir, job.ExportUID+"."+format)
	fail := func(cause error) (*models.ExportJob, error) {
		completed := time.Now().UTC()
		job.Status = "failed"
		job.Error = ptrString(cause.Error())
		job.CompletedAt = &completed
		_ = s.Store.UpdateExport(ctx, job)
		_ = os.Remove(path)
		return job, cause
	}

	var total int
	var err error
	if dataset == "observations" {
		total, err = s.Store.CountObservations(ctx, observationFilter)
	} else {
		total, err = s.Store.CountHarmonized(ctx, harmonizedFilter)
	}
	if err != nil {
		return fail(err)
	}
	if total > s.MaxRows {
		return fail(fmt.Errorf("export would contain %d rows; limit is %d", total, s.MaxRows))
	}

	if dataset == "observations" {
		if format == "csv" {
			err = s.writeObservationCSV(ctx, path, observationFilter)
		} else {
			err = s.writeObservationGeoJSON(ctx, path, observationFilter)
		}
	} else {
		if format == "csv" {
			err = s.writeHarmonizedCSV(ctx, path, harmonizedFilter)
		} else {
			err = s.writeHarmonizedGeoJSON(ctx, path, harmonizedFilter)
		}
	}
	if err != nil {
		return fail(err)
	}

	completed := time.Now().UTC()
	job.Status = "completed"
	job.FilePath = &path
	job.RowCount = total
	job.CompletedAt = &completed
	if err := s.Store.UpdateExport(ctx, job); err != nil {
		return fail(err)
	}
	return job, nil
}

func (s *Service) writeObservationCSV(
	ctx context.Context, path string, filter query.ObservationFilter,
) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write(observationColumns); err != nil {
		return err
	}
	err = s.Store.IterateObservations(ctx, filter, func(observation models.Observation) error {
		record := []string{
			strconv.FormatInt(observation.ID, 10),
			observation.ObservationUID,
			observation.Sensor,
			observation.Satellite,
			observation.Source,
			observation.SourceProduct,
			stringValue(observation.SourceVersion),
			stringValue(observation.Processing),
			strconv.FormatFloat(observation.Latitude, 'f', -1, 64),
			strconv.FormatFloat(observation.Longitude, 'f', -1, 64),
			timeutil.FormatZ(observation.AcquiredAt),
			observation.SourceDate,
			observation.SourceTime,
			stringValue(observation.ConfidenceRaw),
			floatValue(observation.ConfidenceNumeric),
			stringValue(observation.ConfidenceClass),
			stringValue(observation.ConfidenceScheme),
			floatValue(observation.FRP),
			observation.FRPUnits,
			floatValue(observation.Brightness),
			floatValue(observation.BrightnessAux),
			floatValue(observation.Scan),
			floatValue(observation.Track),
			stringValue(observation.DayNight),
			intValue(observation.FireType),
			timeutil.FormatZ(observation.FirstSeenAt),
			timeutil.FormatZ(observation.LastSeenAt),
		}
		return writer.Write(record)
	})
	if err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func (s *Service) writeObservationGeoJSON(
	ctx context.Context, path string, filter query.ObservationFilter,
) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(`{"type":"FeatureCollection","features":[`); err != nil {
		return err
	}
	first := true
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	err = s.Store.IterateObservations(ctx, filter, func(observation models.Observation) error {
		if !first {
			if _, err := file.WriteString(","); err != nil {
				return err
			}
		}
		first = false
		feature := map[string]any{
			"type": "Feature",
			"geometry": map[string]any{
				"type":        "Point",
				"coordinates": []float64{observation.Longitude, observation.Latitude},
			},
			"properties": map[string]any{
				"id":                 observation.ID,
				"observation_uid":    observation.ObservationUID,
				"sensor":             observation.Sensor,
				"satellite":          observation.Satellite,
				"source":             observation.Source,
				"source_product":     observation.SourceProduct,
				"processing":         observation.Processing,
				"acquired_at":        timeutil.FormatZ(observation.AcquiredAt),
				"confidence_raw":     observation.ConfidenceRaw,
				"confidence_numeric": observation.ConfidenceNumeric,
				"confidence_class":   observation.ConfidenceClass,
				"confidence_scheme":  observation.ConfidenceScheme,
				"frp":                observation.FRP,
				"frp_units":          observation.FRPUnits,
				"daynight":           observation.DayNight,
				"fire_type":          observation.FireType,
			},
		}
		return encoder.Encode(feature)
	})
	if err != nil {
		return err
	}
	_, err = file.WriteString(`]}`)
	return err
}

func (s *Service) writeHarmonizedCSV(
	ctx context.Context, path string, filter query.HarmonizedFilter,
) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write(harmonizedColumns); err != nil {
		return err
	}
	err = s.Store.IterateHarmonized(ctx, filter, func(cluster models.HarmonizedObservation) error {
		record := []string{
			strconv.FormatInt(cluster.ID, 10),
			cluster.ClusterUID,
			strconv.FormatInt(cluster.RunID, 10),
			strconv.FormatFloat(cluster.CentroidLatitude, 'f', -1, 64),
			strconv.FormatFloat(cluster.CentroidLongitude, 'f', -1, 64),
			timeutil.FormatZ(cluster.StartTime),
			timeutil.FormatZ(cluster.EndTime),
			strconv.Itoa(cluster.ObservationCount),
			strconv.Itoa(cluster.MODISCount),
			strconv.Itoa(cluster.VIIRSCount),
			string(cluster.Satellites),
			floatValue(cluster.TotalFRP),
			floatValue(cluster.MeanFRP),
			string(cluster.ConfidenceSummary),
			string(cluster.Uncertainty),
			floatValue(cluster.PModisDetect),
			floatValue(cluster.FRPCalibrated),
			stringValue(cluster.PredictionModel),
		}
		return writer.Write(record)
	})
	if err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func (s *Service) writeHarmonizedGeoJSON(
	ctx context.Context, path string, filter query.HarmonizedFilter,
) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(`{"type":"FeatureCollection","features":[`); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	index := 0
	err = s.Store.IterateHarmonized(ctx, filter, func(cluster models.HarmonizedObservation) error {
		if index > 0 {
			if _, err := file.WriteString(","); err != nil {
				return err
			}
		}
		index++
		feature := map[string]any{
			"type": "Feature",
			"geometry": map[string]any{
				"type":        "Point",
				"coordinates": []float64{cluster.CentroidLongitude, cluster.CentroidLatitude},
			},
			"properties": map[string]any{
				"id":                 cluster.ID,
				"cluster_uid":        cluster.ClusterUID,
				"run_id":             cluster.RunID,
				"start_time":         timeutil.FormatZ(cluster.StartTime),
				"end_time":           timeutil.FormatZ(cluster.EndTime),
				"observation_count":  cluster.ObservationCount,
				"modis_count":        cluster.MODISCount,
				"viirs_count":        cluster.VIIRSCount,
				"satellites":         cluster.Satellites,
				"total_frp":          cluster.TotalFRP,
				"mean_frp":           cluster.MeanFRP,
				"confidence_summary": cluster.ConfidenceSummary,
				"uncertainty":        cluster.Uncertainty,
				"p_modis_detect":     cluster.PModisDetect,
				"frp_calibrated":     cluster.FRPCalibrated,
				"prediction_model":   cluster.PredictionModel,
			},
		}
		return encoder.Encode(feature)
	})
	if err != nil {
		return err
	}
	_, err = file.WriteString(`]}`)
	return err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatValue(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func ptrString(value string) *string {
	return &value
}
