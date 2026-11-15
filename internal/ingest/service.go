package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/ptr"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
)

const maxStoredErrors = 100

type Stats struct {
	Fetched    int
	Inserted   int
	Duplicates int
	Rejected   int
}

func CreateRun(
	ctx context.Context,
	repository *store.Store,
	product firms.Product,
	bbox string,
	startDate, endDate time.Time,
	trigger string,
) (*models.IngestionRun, error) {
	parsedBBox, err := query.ParseBBox(bbox)
	if err != nil {
		return nil, err
	}
	if parsedBBox == nil {
		return nil, fmt.Errorf("bbox is required")
	}
	area := parsedBBox.String()
	fingerprint := fingerprint(product.Source, area, startDate, endDate)
	run := &models.IngestionRun{
		RunUID:             uuid.NewString(),
		Status:             "pending",
		Trigger:            trigger,
		Source:             product.Source,
		Sensor:             product.Sensor,
		Product:            ptr.To(product.Product),
		Processing:         ptr.To(product.Processing),
		Area:               area,
		StartDate:          startDate.UTC().Format("2006-01-02"),
		EndDate:            endDate.UTC().Format("2006-01-02"),
		RequestFingerprint: fingerprint,
		RequestedAt:        time.Now().UTC(),
		Errors:             models.JSONText("[]"),
	}
	if err := repository.CreateIngestionRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func IngestProduct(
	ctx context.Context,
	repository *store.Store,
	run *models.IngestionRun,
	product firms.Product,
	bbox string,
	startDate, endDate time.Time,
	client *firms.Client,
) (*models.IngestionRun, error) {
	run.Status = "running"
	now := time.Now().UTC()
	run.StartedAt = &now
	if err := repository.UpdateIngestionRun(ctx, run); err != nil {
		return run, err
	}

	err := client.FetchCSV(ctx, product, bbox, startDate, endDate, func(chunk firms.Chunk) error {
		result, err := firms.ParseCSV(strings.NewReader(chunk.Text), product)
		if err != nil {
			return err
		}
		observations := make([]models.Observation, 0, len(result.Observations))
		for _, observation := range result.Observations {
			observations = append(observations, toModel(observation))
		}
		inserted, duplicates, err := repository.UpsertObservations(ctx, run.ID, observations)
		if err != nil {
			return err
		}

		run.FetchedCount += result.TotalRows
		run.InsertedCount += inserted
		run.DuplicateCount += duplicates
		run.RejectedCount += len(result.Errors)
		run.Errors = appendErrors(run.Errors, result.Errors)
		if err := repository.UpdateIngestionRun(ctx, run); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		completed := time.Now().UTC()
		run.Status = "failed"
		run.Error = ptr.To(err.Error())
		run.CompletedAt = &completed
		_ = repository.UpdateIngestionRun(ctx, run)
		return run, err
	}

	completed := time.Now().UTC()
	run.Status = "completed"
	run.CompletedAt = &completed
	if err := repository.UpdateIngestionRun(ctx, run); err != nil {
		return run, err
	}
	return run, nil
}

func IngestText(
	ctx context.Context,
	repository *store.Store,
	run *models.IngestionRun,
	product firms.Product,
	text string,
) (Stats, error) {
	result, err := firms.ParseCSV(strings.NewReader(text), product)
	if err != nil {
		return Stats{}, err
	}
	observations := make([]models.Observation, 0, len(result.Observations))
	for _, observation := range result.Observations {
		observations = append(observations, toModel(observation))
	}
	inserted, duplicates, err := repository.UpsertObservations(ctx, run.ID, observations)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{
		Fetched:    result.TotalRows,
		Inserted:   inserted,
		Duplicates: duplicates,
		Rejected:   len(result.Errors),
	}
	run.FetchedCount += stats.Fetched
	run.InsertedCount += stats.Inserted
	run.DuplicateCount += stats.Duplicates
	run.RejectedCount += stats.Rejected
	run.Errors = appendErrors(run.Errors, result.Errors)
	if err := repository.UpdateIngestionRun(ctx, run); err != nil {
		return stats, err
	}
	return stats, nil
}

func ExecuteRun(
	ctx context.Context,
	repository *store.Store,
	runID int64,
	client *firms.Client,
) error {
	run, err := repository.GetIngestionRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != "pending" && run.Status != "failed" {
		return fmt.Errorf("run status is %q, not executable", run.Status)
	}
	product, err := firms.GetProduct(run.Source)
	if err != nil {
		return err
	}
	startDate, err := time.Parse("2006-01-02", run.StartDate)
	if err != nil {
		return err
	}
	endDate, err := time.Parse("2006-01-02", run.EndDate)
	if err != nil {
		return err
	}
	_, err = IngestProduct(ctx, repository, run, product, run.Area, startDate, endDate, client)
	return err
}

func toModel(observation firms.NormalizedObservation) models.Observation {
	return models.Observation{
		ObservationUID:    observation.ObservationUID,
		Sensor:            observation.Sensor,
		Satellite:         observation.Satellite,
		SatelliteRaw:      observation.SatelliteRaw,
		Source:            observation.Source,
		SourceProduct:     observation.SourceProduct,
		SourceVersion:     observation.SourceVersion,
		Processing:        ptr.To(observation.Processing),
		Latitude:          observation.Latitude,
		Longitude:         observation.Longitude,
		AcquiredAt:        observation.AcquiredAt,
		SourceDate:        observation.SourceDate,
		SourceTime:        observation.SourceTime,
		ConfidenceRaw:     observation.ConfidenceRaw,
		ConfidenceNumeric: observation.ConfidenceNumeric,
		ConfidenceClass:   observation.ConfidenceClass,
		ConfidenceScheme:  observation.ConfidenceScheme,
		FRP:               observation.FRP,
		FRPUnits:          observation.FRPUnits,
		Brightness:        observation.Brightness,
		BrightnessAux:     observation.BrightnessAux,
		Scan:              observation.Scan,
		Track:             observation.Track,
		DayNight:          observation.DayNight,
		FireType:          observation.FireType,
		Raw:               models.JSONText(observation.Raw),
	}
}

func appendErrors(existing models.JSONText, errors []firms.RowError) models.JSONText {
	var current []firms.RowError
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &current)
	}
	remaining := maxStoredErrors - len(current)
	if remaining <= 0 {
		return existing
	}
	if len(errors) > remaining {
		errors = errors[:remaining]
	}
	current = append(current, errors...)
	encoded, err := json.Marshal(current)
	if err != nil {
		return existing
	}
	return models.JSONText(encoded)
}

func fingerprint(source, area string, start, end time.Time) string {
	canonical := fmt.Sprintf("%s|%s|%s|%s", source, area,
		start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02"))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
