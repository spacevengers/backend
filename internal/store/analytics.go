package store

import (
	"context"
	"database/sql"

	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/timeutil"
)

type SensorCount struct {
	Sensor string `json:"sensor"`
	Count  int    `json:"count"`
}

type SatelliteCount struct {
	Sensor    string `json:"sensor"`
	Satellite string `json:"satellite"`
	Count     int    `json:"count"`
}

type DateCount struct {
	Date   string `json:"date"`
	Sensor string `json:"sensor"`
	Count  int    `json:"count"`
}

type ConfidenceCount struct {
	Sensor string `json:"sensor"`
	Class  string `json:"class"`
	Count  int    `json:"count"`
}

type Coverage struct {
	Sensor string    `json:"sensor"`
	Count  int       `json:"count"`
	First  string    `json:"first_acquired_at"`
	Last   string    `json:"last_acquired_at"`
	BBox   []float64 `json:"bbox,omitempty"`
}

type ProductCount struct {
	Sensor     string  `json:"sensor"`
	Source     string  `json:"source"`
	Product    string  `json:"source_product"`
	Processing *string `json:"processing"`
	Version    *string `json:"source_version"`
	Count      int     `json:"observations"`
}

func (s *Store) CountsBySensor(ctx context.Context, filter query.ObservationFilter) ([]SensorCount, error) {
	where, args := filter.Where()
	statement := "SELECT sensor, COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY sensor ORDER BY sensor"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SensorCount
	for rows.Next() {
		var row SensorCount
		if err := rows.Scan(&row.Sensor, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CountsBySatellite(ctx context.Context, filter query.ObservationFilter) ([]SatelliteCount, error) {
	where, args := filter.Where()
	statement := "SELECT sensor, satellite, COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY sensor, satellite ORDER BY sensor, satellite"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SatelliteCount
	for rows.Next() {
		var row SatelliteCount
		if err := rows.Scan(&row.Sensor, &row.Satellite, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CountsByDate(ctx context.Context, filter query.ObservationFilter) ([]DateCount, error) {
	where, args := filter.Where()
	statement := "SELECT source_date, sensor, COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY source_date, sensor ORDER BY source_date"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DateCount
	for rows.Next() {
		var row DateCount
		if err := rows.Scan(&row.Date, &row.Sensor, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CountsByConfidence(ctx context.Context, filter query.ObservationFilter) ([]ConfidenceCount, error) {
	where, args := filter.Where()
	statement := "SELECT sensor, COALESCE(confidence_class, 'unknown'), COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY sensor, COALESCE(confidence_class, 'unknown') ORDER BY sensor"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfidenceCount
	for rows.Next() {
		var row ConfidenceCount
		if err := rows.Scan(&row.Sensor, &row.Class, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type SourceCount struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

func (s *Store) CountsBySource(ctx context.Context, filter query.ObservationFilter) ([]SourceCount, error) {
	where, args := filter.Where()
	statement := "SELECT source, COUNT(*) FROM observations"
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY source ORDER BY source"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceCount
	for rows.Next() {
		var row SourceCount
		if err := rows.Scan(&row.Source, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) FRPValuesBySensor(ctx context.Context, filter query.ObservationFilter) (map[string][]float64, error) {
	where, args := filter.Where()
	statement := "SELECT sensor, frp FROM observations WHERE frp IS NOT NULL"
	if where != "" {
		statement += " AND (" + where + ")"
	}
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]float64)
	for rows.Next() {
		var sensor string
		var frp float64
		if err := rows.Scan(&sensor, &frp); err != nil {
			return nil, err
		}
		out[sensor] = append(out[sensor], frp)
	}
	return out, rows.Err()
}

func (s *Store) Coverage(ctx context.Context, filter query.ObservationFilter) ([]Coverage, error) {
	where, args := filter.Where()
	statement := `
		SELECT sensor, COUNT(*), MIN(acquired_at), MAX(acquired_at),
			MIN(longitude), MIN(latitude), MAX(longitude), MAX(latitude)
		FROM observations`
	if where != "" {
		statement += " WHERE " + where
	}
	statement += " GROUP BY sensor ORDER BY sensor"
	rows, err := s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Coverage
	for rows.Next() {
		var row Coverage
		var first, last sql.NullInt64
		var west, south, east, north sql.NullFloat64
		if err := rows.Scan(&row.Sensor, &row.Count, &first, &last, &west, &south, &east, &north); err != nil {
			return nil, err
		}
		if first.Valid {
			row.First = timeutil.FormatZ(timeutil.FromMilli(first.Int64))
		}
		if last.Valid {
			row.Last = timeutil.FormatZ(timeutil.FromMilli(last.Int64))
		}
		if west.Valid {
			row.BBox = []float64{west.Float64, south.Float64, east.Float64, north.Float64}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) ProductCounts(ctx context.Context) ([]ProductCount, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT sensor, source, source_product, processing, source_version, COUNT(*)
		FROM observations
		GROUP BY sensor, source, source_product, processing, source_version
		ORDER BY sensor, source, source_version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductCount
	for rows.Next() {
		var row ProductCount
		var processing, version sql.NullString
		if err := rows.Scan(&row.Sensor, &row.Source, &row.Product, &processing, &version, &row.Count); err != nil {
			return nil, err
		}
		row.Processing = nullString(processing)
		row.Version = nullString(version)
		out = append(out, row)
	}
	return out, rows.Err()
}
