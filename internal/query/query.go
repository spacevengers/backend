package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BBox struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func ParseBBox(value string) (*BBox, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if strings.EqualFold(strings.TrimSpace(value), "world") {
		return &BBox{West: -180, South: -90, East: 180, North: 90}, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("bbox must be 'west,south,east,north' or 'world'")
	}
	numbers := make([]float64, 4)
	for index, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("bbox values must be numbers")
		}
		numbers[index] = parsed
	}
	bbox := &BBox{West: numbers[0], South: numbers[1], East: numbers[2], North: numbers[3]}
	if !(-180 <= bbox.West && bbox.West < bbox.East && bbox.East <= 180) {
		return nil, fmt.Errorf("bbox west/east must satisfy -180 <= west < east <= 180")
	}
	if !(-90 <= bbox.South && bbox.South < bbox.North && bbox.North <= 90) {
		return nil, fmt.Errorf("bbox south/north must satisfy -90 <= south < north <= 90")
	}
	return bbox, nil
}

func (b *BBox) String() string {
	if b == nil {
		return "world"
	}
	return fmt.Sprintf("%g,%g,%g,%g", b.West, b.South, b.East, b.North)
}

// ObservationFilter is shared by list, statistics, exports, and harmonization.
type ObservationFilter struct {
	Sensor          string     `json:"sensor,omitempty"`
	Satellite       string     `json:"satellite,omitempty"`
	Source          string     `json:"source,omitempty"`
	Processing      string     `json:"processing,omitempty"`
	Start           *time.Time `json:"start,omitempty"`
	End             *time.Time `json:"end,omitempty"`
	BBox            *BBox      `json:"bbox,omitempty"`
	ConfidenceClass string     `json:"confidence_class,omitempty"`
	MinConfidence   *float64   `json:"min_confidence,omitempty"`
	MaxConfidence   *float64   `json:"max_confidence,omitempty"`
	MinFRP          *float64   `json:"min_frp,omitempty"`
	MaxFRP          *float64   `json:"max_frp,omitempty"`
	DayNight        string     `json:"daynight,omitempty"`
}

// Where returns a SQL clause (without "WHERE") and its arguments.
func (f ObservationFilter) Where() (string, []any) {
	clauses := make([]string, 0, 12)
	args := make([]any, 0, 12)
	if f.Sensor != "" {
		clauses = append(clauses, "sensor = ?")
		args = append(args, strings.ToUpper(f.Sensor))
	}
	if f.Satellite != "" {
		clauses = append(clauses, "satellite = ?")
		args = append(args, f.Satellite)
	}
	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, strings.ToUpper(f.Source))
	}
	if f.Processing != "" {
		clauses = append(clauses, "processing = ?")
		args = append(args, strings.ToUpper(f.Processing))
	}
	if f.Start != nil {
		clauses = append(clauses, "acquired_at >= ?")
		args = append(args, f.Start.UnixMilli())
	}
	if f.End != nil {
		clauses = append(clauses, "acquired_at <= ?")
		args = append(args, f.End.UnixMilli())
	}
	if f.BBox != nil {
		clauses = append(clauses,
			"longitude >= ?", "longitude <= ?", "latitude >= ?", "latitude <= ?")
		args = append(args, f.BBox.West, f.BBox.East, f.BBox.South, f.BBox.North)
	}
	if f.ConfidenceClass != "" {
		clauses = append(clauses, "confidence_class = ?")
		args = append(args, strings.ToLower(f.ConfidenceClass))
	}
	if f.MinConfidence != nil {
		clauses = append(clauses, "confidence_numeric >= ?")
		args = append(args, *f.MinConfidence)
	}
	if f.MaxConfidence != nil {
		clauses = append(clauses, "confidence_numeric <= ?")
		args = append(args, *f.MaxConfidence)
	}
	if f.MinFRP != nil {
		clauses = append(clauses, "frp >= ?")
		args = append(args, *f.MinFRP)
	}
	if f.MaxFRP != nil {
		clauses = append(clauses, "frp <= ?")
		args = append(args, *f.MaxFRP)
	}
	if f.DayNight != "" {
		clauses = append(clauses, "daynight = ?")
		args = append(args, strings.ToUpper(f.DayNight))
	}
	return strings.Join(clauses, " AND "), args
}

type HarmonizedFilter struct {
	RunID        *int64     `json:"run_id,omitempty"`
	Start        *time.Time `json:"start,omitempty"`
	End          *time.Time `json:"end,omitempty"`
	BBox         *BBox      `json:"bbox,omitempty"`
	MinFRP       *float64   `json:"min_frp,omitempty"`
	MaxFRP       *float64   `json:"max_frp,omitempty"`
	ComposedOnly bool       `json:"composed_only,omitempty"`
}

func (f HarmonizedFilter) Where() (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if f.RunID != nil {
		clauses = append(clauses, "run_id = ?")
		args = append(args, *f.RunID)
	}
	if f.Start != nil {
		clauses = append(clauses, "end_time >= ?")
		args = append(args, f.Start.UnixMilli())
	}
	if f.End != nil {
		clauses = append(clauses, "start_time <= ?")
		args = append(args, f.End.UnixMilli())
	}
	if f.BBox != nil {
		clauses = append(clauses,
			"centroid_longitude >= ?", "centroid_longitude <= ?",
			"centroid_latitude >= ?", "centroid_latitude <= ?")
		args = append(args, f.BBox.West, f.BBox.East, f.BBox.South, f.BBox.North)
	}
	if f.MinFRP != nil {
		clauses = append(clauses, "total_frp >= ?")
		args = append(args, *f.MinFRP)
	}
	if f.MaxFRP != nil {
		clauses = append(clauses, "total_frp <= ?")
		args = append(args, *f.MaxFRP)
	}
	if f.ComposedOnly {
		clauses = append(clauses, "modis_count > 0", "viirs_count > 0")
	}
	return strings.Join(clauses, " AND "), args
}
