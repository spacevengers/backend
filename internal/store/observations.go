package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"firms-harmonizer/internal/db"
	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/timeutil"
)

const observationColumns = `
	id, observation_uid, sensor, satellite, satellite_raw, source,
	source_product, source_version, processing, latitude, longitude,
	acquired_at, source_date, source_time, confidence_raw,
	confidence_numeric, confidence_class, confidence_scheme, frp, frp_units,
	brightness, brightness_aux, scan, track, daynight, fire_type, raw,
	first_seen_at, last_seen_at, ingested_at`

type obsScan struct {
	id                                     int64
	observationUID, sensor, satellite      string
	satelliteRaw                           sql.NullString
	source, sourceProduct                  string
	sourceVersion, processing              sql.NullString
	latitude, longitude                    float64
	acquiredAt                             int64
	sourceDate, sourceTime                 string
	confidenceRaw, confidenceClass         sql.NullString
	confidenceScheme                       sql.NullString
	confidenceNumeric, frp                 sql.NullFloat64
	frpUnits                               string
	brightness, brightnessAux, scan, track sql.NullFloat64
	dayNight                               sql.NullString
	fireType                               sql.NullInt64
	raw                                    string
	firstSeenAt, lastSeenAt, ingestedAt    int64
}

func (o *obsScan) dest() []any {
	return []any{
		&o.id, &o.observationUID, &o.sensor, &o.satellite, &o.satelliteRaw,
		&o.source, &o.sourceProduct, &o.sourceVersion, &o.processing,
		&o.latitude, &o.longitude, &o.acquiredAt, &o.sourceDate, &o.sourceTime,
		&o.confidenceRaw, &o.confidenceNumeric, &o.confidenceClass,
		&o.confidenceScheme, &o.frp, &o.frpUnits, &o.brightness,
		&o.brightnessAux, &o.scan, &o.track, &o.dayNight, &o.fireType, &o.raw,
		&o.firstSeenAt, &o.lastSeenAt, &o.ingestedAt,
	}
}

func (o *obsScan) model() models.Observation {
	return models.Observation{
		ID:                o.id,
		ObservationUID:    o.observationUID,
		Sensor:            o.sensor,
		Satellite:         o.satellite,
		SatelliteRaw:      nullString(o.satelliteRaw),
		Source:            o.source,
		SourceProduct:     o.sourceProduct,
		SourceVersion:     nullString(o.sourceVersion),
		Processing:        nullString(o.processing),
		Latitude:          o.latitude,
		Longitude:         o.longitude,
		AcquiredAt:        timeutil.FromMilli(o.acquiredAt),
		SourceDate:        o.sourceDate,
		SourceTime:        o.sourceTime,
		ConfidenceRaw:     nullString(o.confidenceRaw),
		ConfidenceNumeric: nullFloat(o.confidenceNumeric),
		ConfidenceClass:   nullString(o.confidenceClass),
		ConfidenceScheme:  nullString(o.confidenceScheme),
		FRP:               nullFloat(o.frp),
		FRPUnits:          o.frpUnits,
		Brightness:        nullFloat(o.brightness),
		BrightnessAux:     nullFloat(o.brightnessAux),
		Scan:              nullFloat(o.scan),
		Track:             nullFloat(o.track),
		DayNight:          nullString(o.dayNight),
		FireType:          nullInt(o.fireType),
		Raw:               models.JSONText(o.raw),
		FirstSeenAt:       timeutil.FromMilli(o.firstSeenAt),
		LastSeenAt:        timeutil.FromMilli(o.lastSeenAt),
		IngestedAt:        timeutil.FromMilli(o.ingestedAt),
	}
}

func nullFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func (s *Store) UpsertObservations(
	ctx context.Context, runID int64, observations []models.Observation,
) (inserted, duplicates int, err error) {
	if len(observations) == 0 {
		return 0, 0, nil
	}

	unique := make(map[string]models.Observation, len(observations))
	for _, observation := range observations {
		if _, exists := unique[observation.ObservationUID]; exists {
			duplicates++
			continue
		}
		unique[observation.ObservationUID] = observation
	}

	uids := make([]string, 0, len(unique))
	for uid := range unique {
		uids = append(uids, uid)
	}

	err = db.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		existing := make(map[string]struct{}, len(uids))
		for start := 0; start < len(uids); start += 400 {
			end := min(start+400, len(uids))
			chunk := uids[start:end]
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
			args := make([]any, 0, len(chunk))
			for _, uid := range chunk {
				args = append(args, uid)
			}
			rows, err := tx.QueryContext(ctx,
				"SELECT observation_uid FROM observations WHERE observation_uid IN ("+placeholders+")",
				args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var uid string
				if err := rows.Scan(&uid); err != nil {
					rows.Close()
					return err
				}
				existing[uid] = struct{}{}
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}

		now := time.Now().UTC()
		updateStmt, err := tx.PrepareContext(ctx, `
			UPDATE observations SET last_seen_at = ?, last_ingestion_run_id = ?
			WHERE observation_uid = ?`)
		if err != nil {
			return err
		}
		defer updateStmt.Close()
		insertStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO observations (
				observation_uid, sensor, satellite, satellite_raw, source,
				source_product, source_version, processing, latitude, longitude,
				acquired_at, source_date, source_time, confidence_raw,
				confidence_numeric, confidence_class, confidence_scheme, frp,
				frp_units, brightness, brightness_aux, scan, track, daynight,
				fire_type, raw, first_ingestion_run_id, last_ingestion_run_id,
				first_seen_at, last_seen_at, ingested_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		for _, observation := range unique {
			if _, ok := existing[observation.ObservationUID]; ok {
				duplicates++
				if _, err := updateStmt.ExecContext(ctx,
					timeutil.Milli(now), runID, observation.ObservationUID); err != nil {
					return err
				}
				continue
			}
			if _, err := insertStmt.ExecContext(ctx,
				observation.ObservationUID, observation.Sensor, observation.Satellite,
				observation.SatelliteRaw, observation.Source, observation.SourceProduct,
				observation.SourceVersion, observation.Processing, observation.Latitude,
				observation.Longitude, timeutil.Milli(observation.AcquiredAt),
				observation.SourceDate, observation.SourceTime, observation.ConfidenceRaw,
				observation.ConfidenceNumeric, observation.ConfidenceClass,
				observation.ConfidenceScheme, observation.FRP, observation.FRPUnits,
				observation.Brightness, observation.BrightnessAux, observation.Scan,
				observation.Track, observation.DayNight, observation.FireType,
				string(observation.Raw), runID, runID, timeutil.Milli(now),
				timeutil.Milli(now), timeutil.Milli(now)); err != nil {
				return err
			}
			inserted++
		}
		return nil
	})
	return inserted, duplicates, err
}

func (s *Store) CountObservations(ctx context.Context, filter query.ObservationFilter) (int, error) {
	where, args := filter.Where()
	statement := "SELECT COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	var total int
	err := s.DB.QueryRowContext(ctx, statement, args...).Scan(&total)
	return total, err
}

func (s *Store) ListObservations(
	ctx context.Context,
	filter query.ObservationFilter,
	sortBy, order string,
	limit, offset int,
) ([]models.Observation, error) {
	column := map[string]string{
		"acquired_at":        "acquired_at",
		"frp":                "frp",
		"confidence_numeric": "confidence_numeric",
	}[sortBy]
	if column == "" {
		column = "acquired_at"
	}
	direction := "DESC"
	if strings.EqualFold(order, "asc") {
		direction = "ASC"
	}
	where, args := filter.Where()
	statement := "SELECT " + observationColumns + " FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += fmt.Sprintf(
		" ORDER BY (%s IS NULL) ASC, %s %s, id DESC LIMIT ? OFFSET ?",
		column, column, direction)
	args = append(args, limit, offset)

	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Observation, 0, limit)
	for rows.Next() {
		var scan obsScan
		if err := rows.Scan(scan.dest()...); err != nil {
			return nil, err
		}
		out = append(out, scan.model())
	}
	return out, rows.Err()
}

func (s *Store) IterateObservations(
	ctx context.Context,
	filter query.ObservationFilter,
	consume func(models.Observation) error,
) error {
	where, args := filter.Where()
	statement := "SELECT " + observationColumns + " FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " ORDER BY acquired_at ASC, id ASC"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var scan obsScan
		if err := rows.Scan(scan.dest()...); err != nil {
			return err
		}
		if err := consume(scan.model()); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) GetObservation(ctx context.Context, id int64) (*models.Observation, error) {
	row := s.DB.QueryRowContext(ctx,
		"SELECT "+observationColumns+" FROM observations WHERE id = ?", id)
	var scan obsScan
	if err := row.Scan(scan.dest()...); err != nil {
		return nil, err
	}
	observation := scan.model()
	return &observation, nil
}

func (s *Store) ObservationPairs(
	ctx context.Context,
	filter query.ObservationFilter,
	limit int,
) ([]models.Observation, error) {
	return s.ListObservations(ctx, filter, "acquired_at", "asc", limit, 0)
}
