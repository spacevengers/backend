package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/timeutil"
)

type Store struct {
	DB *sql.DB
}

func New(database *sql.DB) *Store {
	return &Store{DB: database}
}

func (s *Store) CreateIngestionRun(ctx context.Context, run *models.IngestionRun) error {
	if run.RequestedAt.IsZero() {
		run.RequestedAt = time.Now().UTC()
	}
	if len(run.Errors) == 0 {
		run.Errors = models.JSONText("[]")
	}
	result, err := s.DB.ExecContext(ctx, `
		INSERT INTO ingestion_runs (
			run_uid, status, trigger, source, sensor, product, processing, area,
			start_date, end_date, request_fingerprint, requested_at, started_at,
			completed_at, fetched_count, inserted_count, duplicate_count,
			rejected_count, error, errors
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunUID, run.Status, run.Trigger, run.Source, run.Sensor, run.Product,
		run.Processing, run.Area, run.StartDate, run.EndDate, run.RequestFingerprint,
		timeutil.Milli(run.RequestedAt), nullableMilli(run.StartedAt),
		nullableMilli(run.CompletedAt), run.FetchedCount, run.InsertedCount,
		run.DuplicateCount, run.RejectedCount, run.Error, string(run.Errors))
	if err != nil {
		return err
	}
	run.ID, err = result.LastInsertId()
	return err
}

func (s *Store) UpdateIngestionRun(ctx context.Context, run *models.IngestionRun) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE ingestion_runs SET
			status = ?, started_at = ?, completed_at = ?, fetched_count = ?,
			inserted_count = ?, duplicate_count = ?, rejected_count = ?,
			error = ?, errors = ?
		WHERE id = ?`,
		run.Status, nullableMilli(run.StartedAt), nullableMilli(run.CompletedAt),
		run.FetchedCount, run.InsertedCount, run.DuplicateCount, run.RejectedCount,
		run.Error, string(run.Errors), run.ID)
	return err
}

func (s *Store) GetIngestionRun(ctx context.Context, id int64) (*models.IngestionRun, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, run_uid, status, trigger, source, sensor, product, processing,
			area, start_date, end_date, request_fingerprint, requested_at,
			started_at, completed_at, fetched_count, inserted_count,
			duplicate_count, rejected_count, error, errors
		FROM ingestion_runs WHERE id = ?`, id)
	return scanIngestionRun(row)
}

func (s *Store) ListIngestionRuns(
	ctx context.Context, status, source string, limit, offset int,
) ([]models.IngestionRun, int, error) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, strings.ToLower(status))
	}
	if source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, strings.ToUpper(source))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := s.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ingestion_runs"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT id, run_uid, status, trigger, source, sensor, product, processing,
			area, start_date, end_date, request_fingerprint, requested_at,
			started_at, completed_at, fetched_count, inserted_count,
			duplicate_count, rejected_count, error, errors
		FROM ingestion_runs` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.DB.QueryContext(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.IngestionRun
	for rows.Next() {
		run, err := scanIngestionRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *run)
	}
	return out, total, rows.Err()
}

func scanIngestionRun(scanner interface{ Scan(...any) error }) (*models.IngestionRun, error) {
	var run models.IngestionRun
	var requestedAt, startedAt, completedAt sql.NullInt64
	var product, processing, errorText sql.NullString
	var errorsJSON string
	err := scanner.Scan(
		&run.ID, &run.RunUID, &run.Status, &run.Trigger, &run.Source, &run.Sensor,
		&product, &processing, &run.Area, &run.StartDate, &run.EndDate,
		&run.RequestFingerprint, &requestedAt, &startedAt, &completedAt,
		&run.FetchedCount, &run.InsertedCount, &run.DuplicateCount,
		&run.RejectedCount, &errorText, &errorsJSON)
	if err != nil {
		return nil, err
	}
	run.Product = nullString(product)
	run.Processing = nullString(processing)
	run.Error = nullString(errorText)
	run.RequestedAt = timeutil.FromMilli(requestedAt.Int64)
	run.StartedAt = timeutil.FromMilliPtr(nullInt64(startedAt))
	run.CompletedAt = timeutil.FromMilliPtr(nullInt64(completedAt))
	run.Errors = models.JSONText(errorsJSON)
	return &run, nil
}

func (s *Store) CreateExport(ctx context.Context, job *models.ExportJob) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if len(job.Filters) == 0 {
		job.Filters = models.JSONText("{}")
	}
	result, err := s.DB.ExecContext(ctx, `
		INSERT INTO export_jobs (
			export_uid, status, dataset, format, filters, file_path, row_count,
			error, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ExportUID, job.Status, job.Dataset, job.Format, string(job.Filters),
		job.FilePath, job.RowCount, job.Error, timeutil.Milli(job.CreatedAt),
		nullableMilli(job.CompletedAt))
	if err != nil {
		return err
	}
	job.ID, err = result.LastInsertId()
	return err
}

func (s *Store) UpdateExport(ctx context.Context, job *models.ExportJob) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE export_jobs SET status = ?, file_path = ?, row_count = ?, error = ?,
			completed_at = ? WHERE id = ?`,
		job.Status, job.FilePath, job.RowCount, job.Error,
		nullableMilli(job.CompletedAt), job.ID)
	return err
}

func (s *Store) GetExportByUID(ctx context.Context, uid string) (*models.ExportJob, error) {
	return scanExport(s.DB.QueryRowContext(ctx, `
		SELECT id, export_uid, status, dataset, format, filters, file_path,
			row_count, error, created_at, completed_at
		FROM export_jobs WHERE export_uid = ?`, uid))
}

func (s *Store) ListExports(ctx context.Context, limit, offset int) ([]models.ExportJob, int, error) {
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM export_jobs").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, export_uid, status, dataset, format, filters, file_path,
			row_count, error, created_at, completed_at
		FROM export_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.ExportJob
	for rows.Next() {
		job, err := scanExport(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *job)
	}
	return out, total, rows.Err()
}

func scanExport(scanner interface{ Scan(...any) error }) (*models.ExportJob, error) {
	var job models.ExportJob
	var filtersJSON string
	var filePath, errorText sql.NullString
	var createdAt int64
	var completedAt sql.NullInt64
	err := scanner.Scan(&job.ID, &job.ExportUID, &job.Status, &job.Dataset,
		&job.Format, &filtersJSON, &filePath, &job.RowCount, &errorText,
		&createdAt, &completedAt)
	if err != nil {
		return nil, err
	}
	job.Filters = models.JSONText(filtersJSON)
	job.FilePath = nullString(filePath)
	job.Error = nullString(errorText)
	job.CreatedAt = timeutil.FromMilli(createdAt)
	job.CompletedAt = timeutil.FromMilliPtr(nullInt64(completedAt))
	return &job, nil
}

func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableMilli(value *time.Time) any {
	if value == nil {
		return nil
	}
	return timeutil.Milli(*value)
}

func JSON(value any) models.JSONText {
	encoded, err := json.Marshal(value)
	if err != nil {
		return models.JSONText("null")
	}
	return models.JSONText(encoded)
}

func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

func (s *Store) Close() error {
	return s.DB.Close()
}
