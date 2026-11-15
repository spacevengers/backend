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

type Prediction struct {
	ClusterUID    string   `json:"cluster_uid"`
	PModisDetect  *float64 `json:"p_modis_detect,omitempty"`
	FRPCalibrated *float64 `json:"frp_calibrated,omitempty"`
}

func (s *Store) CreateHarmonizationRun(ctx context.Context, run *models.HarmonizationRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if len(run.Params) == 0 {
		run.Params = models.JSONText("{}")
	}
	if len(run.Filters) == 0 {
		run.Filters = models.JSONText("{}")
	}
	if len(run.Metrics) == 0 {
		run.Metrics = models.JSONText("{}")
	}
	result, err := s.DB.ExecContext(ctx, `
		INSERT INTO harmonization_runs (
			run_uid, method, status, params, filters, started_at, completed_at,
			input_count, cluster_count, match_count, metrics, error, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunUID, run.Method, run.Status, string(run.Params), string(run.Filters),
		nullableMilli(run.StartedAt), nullableMilli(run.CompletedAt),
		run.InputCount, run.ClusterCount, run.MatchCount, string(run.Metrics),
		run.Error, timeutil.Milli(run.CreatedAt))
	if err != nil {
		return err
	}
	run.ID, err = result.LastInsertId()
	return err
}

func (s *Store) UpdateHarmonizationRun(ctx context.Context, run *models.HarmonizationRun) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE harmonization_runs SET
			status = ?, params = ?, filters = ?, started_at = ?, completed_at = ?,
			input_count = ?, cluster_count = ?, match_count = ?, metrics = ?,
			error = ?
		WHERE id = ?`,
		run.Status, string(run.Params), string(run.Filters),
		nullableMilli(run.StartedAt), nullableMilli(run.CompletedAt),
		run.InputCount, run.ClusterCount, run.MatchCount, string(run.Metrics),
		run.Error, run.ID)
	return err
}

func (s *Store) GetHarmonizationRun(ctx context.Context, id int64) (*models.HarmonizationRun, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, run_uid, method, status, params, filters, started_at,
			completed_at, input_count, cluster_count, match_count, metrics,
			error, created_at
		FROM harmonization_runs WHERE id = ?`, id)
	return scanHarmonizationRun(row)
}

func (s *Store) ListHarmonizationRuns(
	ctx context.Context, status string, limit, offset int,
) ([]models.HarmonizationRun, int, error) {
	clauses := make([]string, 0, 1)
	args := make([]any, 0, 3)
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, strings.ToLower(status))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := s.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM harmonization_runs"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, run_uid, method, status, params, filters, started_at,
			completed_at, input_count, cluster_count, match_count, metrics,
			error, created_at
		FROM harmonization_runs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.HarmonizationRun
	for rows.Next() {
		run, err := scanHarmonizationRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *run)
	}
	return out, total, rows.Err()
}

func (s *Store) LatestCompletedRunID(ctx context.Context) (*int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx,
		"SELECT id FROM harmonization_runs WHERE status = 'completed' ORDER BY id DESC LIMIT 1").Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func scanHarmonizationRun(scanner interface{ Scan(...any) error }) (*models.HarmonizationRun, error) {
	var run models.HarmonizationRun
	var params, filters, metrics string
	var startedAt, completedAt sql.NullInt64
	var errorText sql.NullString
	var createdAt int64
	err := scanner.Scan(&run.ID, &run.RunUID, &run.Method, &run.Status, &params,
		&filters, &startedAt, &completedAt, &run.InputCount, &run.ClusterCount,
		&run.MatchCount, &metrics, &errorText, &createdAt)
	if err != nil {
		return nil, err
	}
	run.Params = models.JSONText(params)
	run.Filters = models.JSONText(filters)
	run.Metrics = models.JSONText(metrics)
	run.StartedAt = timeutil.FromMilliPtr(nullInt64(startedAt))
	run.CompletedAt = timeutil.FromMilliPtr(nullInt64(completedAt))
	run.Error = nullString(errorText)
	run.CreatedAt = timeutil.FromMilli(createdAt)
	return &run, nil
}

func (s *Store) ReplaceHarmonizationOutputs(
	ctx context.Context,
	runID int64,
	clusters []models.HarmonizedObservation,
	matches []models.ObservationMatch,
) error {
	return db.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM harmonized_observations WHERE run_id = ?", runID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM observation_matches WHERE run_id = ?", runID); err != nil {
			return err
		}

		now := time.Now().UTC()
		clusterStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO harmonized_observations (
				cluster_uid, run_id, centroid_latitude, centroid_longitude,
				start_time, end_time, observation_count, modis_count, viirs_count,
				satellites, total_frp, mean_frp, confidence_summary, uncertainty,
				p_modis_detect, frp_calibrated, prediction_model, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer clusterStmt.Close()
		memberStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO harmonized_members (harmonized_id, observation_id, role)
			VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer memberStmt.Close()

		for index := range clusters {
			cluster := &clusters[index]
			result, err := clusterStmt.ExecContext(ctx,
				cluster.ClusterUID, runID, cluster.CentroidLatitude,
				cluster.CentroidLongitude, timeutil.Milli(cluster.StartTime),
				timeutil.Milli(cluster.EndTime), cluster.ObservationCount,
				cluster.MODISCount, cluster.VIIRSCount, string(cluster.Satellites),
				cluster.TotalFRP, cluster.MeanFRP, string(cluster.ConfidenceSummary),
				string(cluster.Uncertainty), cluster.PModisDetect, cluster.FRPCalibrated,
				cluster.PredictionModel, timeutil.Milli(now))
			if err != nil {
				return err
			}
			cluster.ID, err = result.LastInsertId()
			if err != nil {
				return err
			}
			for _, member := range cluster.Members {
				if _, err := memberStmt.ExecContext(ctx,
					cluster.ID, member.ObservationID, member.Role); err != nil {
					return err
				}
			}
		}

		matchStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO observation_matches (
				run_id, modis_observation_id, viirs_observation_id, distance_km,
				time_delta_minutes, method, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer matchStmt.Close()
		for _, match := range matches {
			method := match.Method
			if method == "" {
				method = "baseline"
			}
			if _, err := matchStmt.ExecContext(ctx,
				runID, match.MODISObservationID, match.VIIRSObservationID,
				match.DistanceKm, match.TimeDeltaMinutes, method,
				timeutil.Milli(now)); err != nil {
				return err
			}
		}
		return nil
	})
}

const harmonizedColumns = `
	id, cluster_uid, run_id, centroid_latitude, centroid_longitude,
	start_time, end_time, observation_count, modis_count, viirs_count,
	satellites, total_frp, mean_frp, confidence_summary, uncertainty,
	p_modis_detect, frp_calibrated, prediction_model, created_at`

type clusterScan struct {
	id                     int64
	clusterUID             string
	runID                  int64
	centroidLatitude       float64
	centroidLongitude      float64
	startTime, endTime     int64
	observationCount       int
	modisCount, viirsCount int
	satellites             string
	totalFRP, meanFRP      sql.NullFloat64
	confidenceSummary      string
	uncertainty            string
	pModisDetect           sql.NullFloat64
	frpCalibrated          sql.NullFloat64
	predictionModel        sql.NullString
	createdAt              int64
}

func (c *clusterScan) dest() []any {
	return []any{
		&c.id, &c.clusterUID, &c.runID, &c.centroidLatitude,
		&c.centroidLongitude, &c.startTime, &c.endTime, &c.observationCount,
		&c.modisCount, &c.viirsCount, &c.satellites, &c.totalFRP, &c.meanFRP,
		&c.confidenceSummary, &c.uncertainty, &c.pModisDetect, &c.frpCalibrated,
		&c.predictionModel, &c.createdAt,
	}
}

func (c *clusterScan) model() models.HarmonizedObservation {
	return models.HarmonizedObservation{
		ID:                c.id,
		ClusterUID:        c.clusterUID,
		RunID:             c.runID,
		CentroidLatitude:  c.centroidLatitude,
		CentroidLongitude: c.centroidLongitude,
		StartTime:         timeutil.FromMilli(c.startTime),
		EndTime:           timeutil.FromMilli(c.endTime),
		ObservationCount:  c.observationCount,
		MODISCount:        c.modisCount,
		VIIRSCount:        c.viirsCount,
		Satellites:        models.JSONText(c.satellites),
		TotalFRP:          nullFloat(c.totalFRP),
		MeanFRP:           nullFloat(c.meanFRP),
		ConfidenceSummary: models.JSONText(c.confidenceSummary),
		Uncertainty:       models.JSONText(c.uncertainty),
		PModisDetect:      nullFloat(c.pModisDetect),
		FRPCalibrated:     nullFloat(c.frpCalibrated),
		PredictionModel:   nullString(c.predictionModel),
		CreatedAt:         timeutil.FromMilli(c.createdAt),
		Members:           []models.HarmonizedMember{},
	}
}

type ClusterMembers struct {
	MemberIDs []int64
	FRPVIIRS  *float64
	FRPMODIS  *float64
}

func (s *Store) MemberAggregates(
	ctx context.Context, harmonizedIDs []int64,
) (map[int64]ClusterMembers, error) {
	out := make(map[int64]ClusterMembers, len(harmonizedIDs))
	if len(harmonizedIDs) == 0 {
		return out, nil
	}
	for start := 0; start < len(harmonizedIDs); start += 400 {
		end := min(start+400, len(harmonizedIDs))
		chunk := harmonizedIDs[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.DB.QueryContext(ctx, `
			SELECT hm.harmonized_id, hm.observation_id, hm.role, o.frp
			FROM harmonized_members hm
			JOIN observations o ON o.id = hm.observation_id
			WHERE hm.harmonized_id IN (`+placeholders+`)
			ORDER BY hm.id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var harmonizedID, observationID int64
			var role string
			var frp sql.NullFloat64
			if err := rows.Scan(&harmonizedID, &observationID, &role, &frp); err != nil {
				rows.Close()
				return nil, err
			}
			aggregate := out[harmonizedID]
			aggregate.MemberIDs = append(aggregate.MemberIDs, observationID)
			if frp.Valid {
				switch role {
				case "viirs":
					value := frp.Float64
					if aggregate.FRPVIIRS == nil {
						aggregate.FRPVIIRS = &value
					} else {
						*aggregate.FRPVIIRS += value
					}
				case "modis":
					value := frp.Float64
					if aggregate.FRPMODIS == nil {
						aggregate.FRPMODIS = &value
					} else {
						*aggregate.FRPMODIS += value
					}
				}
			}
			out[harmonizedID] = aggregate
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type ClusterPrediction struct {
	ClusterUID    string
	MemberIDs     []int64
	PModisDetect  *float64
	FRPCalibrated *float64
	MatchStatus   string
}

func (s *Store) ClusterPredictions(
	ctx context.Context, runID int64,
) ([]ClusterPrediction, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ho.cluster_uid, hm.observation_id, ho.p_modis_detect,
			ho.frp_calibrated, ho.modis_count, ho.viirs_count
		FROM harmonized_observations ho
		JOIN harmonized_members hm ON hm.harmonized_id = ho.id
		WHERE ho.run_id = ?
		ORDER BY ho.id, hm.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byUID := make(map[string]*ClusterPrediction)
	order := make([]string, 0)
	for rows.Next() {
		var clusterUID string
		var observationID int64
		var pModis, frpCalibrated sql.NullFloat64
		var modisCount, viirsCount int
		if err := rows.Scan(&clusterUID, &observationID, &pModis, &frpCalibrated,
			&modisCount, &viirsCount); err != nil {
			return nil, err
		}
		entry, ok := byUID[clusterUID]
		if !ok {
			entry = &ClusterPrediction{ClusterUID: clusterUID}
			switch {
			case modisCount > 0 && viirsCount > 0:
				entry.MatchStatus = "matched"
			case modisCount > 0:
				entry.MatchStatus = "modis_only"
			default:
				entry.MatchStatus = "viirs_only"
			}
			entry.PModisDetect = nullFloat(pModis)
			entry.FRPCalibrated = nullFloat(frpCalibrated)
			byUID[clusterUID] = entry
			order = append(order, clusterUID)
		}
		entry.MemberIDs = append(entry.MemberIDs, observationID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ClusterPrediction, 0, len(order))
	for _, uid := range order {
		out = append(out, *byUID[uid])
	}
	return out, nil
}

func (s *Store) UpdatePredictions(
	ctx context.Context,
	runID int64,
	model string,
	predictions []Prediction,
) (int, error) {
	if model == "" {
		model = "external"
	}
	seen := make(map[string]struct{}, len(predictions))
	err := db.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, `
			UPDATE harmonized_observations SET
				p_modis_detect = COALESCE(?, p_modis_detect),
				frp_calibrated = COALESCE(?, frp_calibrated),
				prediction_model = ?
			WHERE run_id = ? AND cluster_uid = ?`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for _, prediction := range predictions {
			if prediction.ClusterUID == "" {
				return fmt.Errorf("cluster_uid is required")
			}
			if _, duplicate := seen[prediction.ClusterUID]; duplicate {
				return fmt.Errorf("duplicate cluster_uid %q", prediction.ClusterUID)
			}
			seen[prediction.ClusterUID] = struct{}{}
			if prediction.PModisDetect == nil && prediction.FRPCalibrated == nil {
				return fmt.Errorf("cluster %q has no prediction values", prediction.ClusterUID)
			}
			if prediction.PModisDetect != nil &&
				(*prediction.PModisDetect < 0 || *prediction.PModisDetect > 1) {
				return fmt.Errorf("cluster %q p_modis_detect must be in [0, 1]", prediction.ClusterUID)
			}
			if prediction.FRPCalibrated != nil && *prediction.FRPCalibrated < 0 {
				return fmt.Errorf("cluster %q frp_calibrated must be non-negative", prediction.ClusterUID)
			}
			result, err := statement.ExecContext(ctx,
				prediction.PModisDetect, prediction.FRPCalibrated, model, runID,
				prediction.ClusterUID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return fmt.Errorf("cluster %q not found in run %d", prediction.ClusterUID, runID)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(predictions), nil
}

func (s *Store) CountHarmonizedByStatus(
	ctx context.Context, filter query.HarmonizedFilter,
) (map[string]int, error) {
	where, args := filter.Where()
	statement := `
		SELECT CASE
			WHEN modis_count > 0 AND viirs_count > 0 THEN 'matched'
			WHEN modis_count > 0 THEN 'modis_only'
			ELSE 'viirs_only'
		END AS status, COUNT(*)
		FROM harmonized_observations`
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY status"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"matched": 0, "modis_only": 0, "viirs_only": 0}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

func (s *Store) CountHarmonized(ctx context.Context, filter query.HarmonizedFilter) (int, error) {
	where, args := filter.Where()
	statement := "SELECT COUNT(*) FROM harmonized_observations"
	if where != "" {
		statement += " WHERE " + where
	}
	var total int
	err := s.DB.QueryRowContext(ctx, statement, args...).Scan(&total)
	return total, err
}

func (s *Store) ListHarmonized(
	ctx context.Context, filter query.HarmonizedFilter, limit, offset int,
) ([]models.HarmonizedObservation, error) {
	where, args := filter.Where()
	statement := "SELECT " + harmonizedColumns + " FROM harmonized_observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " ORDER BY start_time DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.HarmonizedObservation, 0, limit)
	for rows.Next() {
		var scan clusterScan
		if err := rows.Scan(scan.dest()...); err != nil {
			return nil, err
		}
		out = append(out, scan.model())
	}
	return out, rows.Err()
}

func (s *Store) IterateHarmonized(
	ctx context.Context, filter query.HarmonizedFilter,
	consume func(models.HarmonizedObservation) error,
) error {
	where, args := filter.Where()
	statement := "SELECT " + harmonizedColumns + " FROM harmonized_observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " ORDER BY start_time ASC, id ASC"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var scan clusterScan
		if err := rows.Scan(scan.dest()...); err != nil {
			return err
		}
		if err := consume(scan.model()); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) GetHarmonized(ctx context.Context, id int64) (*models.HarmonizedObservation, error) {
	row := s.DB.QueryRowContext(ctx,
		"SELECT "+harmonizedColumns+" FROM harmonized_observations WHERE id = ?", id)
	var scan clusterScan
	if err := row.Scan(scan.dest()...); err != nil {
		return nil, err
	}
	cluster := scan.model()

	memberRows, err := s.DB.QueryContext(ctx,
		"SELECT observation_id, role FROM harmonized_members WHERE harmonized_id = ? ORDER BY id",
		id)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var member models.HarmonizedMember
		member.HarmonizedID = id
		if err := memberRows.Scan(&member.ObservationID, &member.Role); err != nil {
			return nil, err
		}
		observation, err := s.GetObservation(ctx, member.ObservationID)
		if err != nil {
			return nil, err
		}
		member.Observation = observation
		cluster.Members = append(cluster.Members, member)
	}
	return &cluster, memberRows.Err()
}

func (s *Store) MatchesForRun(ctx context.Context, runID int64) ([]models.ObservationMatch, error) {
	queryText := `
		SELECT m.id, m.run_id, m.modis_observation_id, m.viirs_observation_id,
			m.distance_km, m.time_delta_minutes, m.method, m.created_at,
			` + prefixedColumns("mo") + `,
			` + prefixedColumns("vo") + `
		FROM observation_matches m
		JOIN observations mo ON mo.id = m.modis_observation_id
		JOIN observations vo ON vo.id = m.viirs_observation_id
		WHERE m.run_id = ?
		ORDER BY m.id`
	rows, err := s.DB.QueryContext(ctx, queryText, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ObservationMatch
	for rows.Next() {
		var match models.ObservationMatch
		var createdAt int64
		var modisScan, viirsScan obsScan
		dest := []any{
			&match.ID, &match.RunID, &match.MODISObservationID,
			&match.VIIRSObservationID, &match.DistanceKm,
			&match.TimeDeltaMinutes, &match.Method, &createdAt,
		}
		dest = append(dest, modisScan.dest()...)
		dest = append(dest, viirsScan.dest()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		match.CreatedAt = timeutil.FromMilli(createdAt)
		modis := modisScan.model()
		viirs := viirsScan.model()
		match.MODIS = &modis
		match.VIIRS = &viirs
		out = append(out, match)
	}
	return out, rows.Err()
}

func prefixedColumns(alias string) string {
	parts := strings.Split(observationColumns, ",")
	for index, part := range parts {
		parts[index] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
