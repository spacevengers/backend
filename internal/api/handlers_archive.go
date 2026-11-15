package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/timeutil"
)

type archiveMonthResult struct {
	Month    string
	Summary  map[string]any
	Compact  compactData
	CachedAt time.Time
}

const archiveTTL = 10 * time.Minute

func (s *Server) archiveMonthResult(
	ctx context.Context, bbox *query.BBox, month string,
) (archiveMonthResult, error) {
	key := month
	if bbox != nil {
		key = bbox.String() + "|" + month
	}
	s.archiveMu.Lock()
	cached, ok := s.archiveCache[key]
	s.archiveMu.Unlock()
	if ok && time.Since(cached.CachedAt) < archiveTTL {
		return cached, nil
	}

	start, err := time.Parse("2006-01", month)
	if err != nil {
		return archiveMonthResult{}, fmt.Errorf("month must be YYYY-MM")
	}
	end := start.AddDate(0, 1, 0).Add(-time.Millisecond)
	filter := query.ObservationFilter{Start: &start, End: &end, BBox: bbox}
	total, err := s.Store.CountObservations(ctx, filter)
	if err != nil {
		return archiveMonthResult{}, err
	}
	if total > 100000 {
		return archiveMonthResult{}, fmt.Errorf(
			"month %s has %d observations; narrow the bounding box", month, total)
	}
	rows, err := s.Store.ListObservations(ctx, filter, "acquired_at", "asc", 100000, 0)
	if err != nil {
		return archiveMonthResult{}, err
	}
	compact := buildCompact(rows, 1.5, 30, nil)
	result := archiveMonthResult{
		Month:    month,
		Summary:  summarizeCompact(compact),
		Compact:  compact,
		CachedAt: time.Now().UTC(),
	}

	s.archiveMu.Lock()
	s.archiveCache[key] = result
	s.archiveMu.Unlock()
	return result, nil
}

func (s *Server) handleIntegrationArchiveSummary(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	startMonth := values.Get("start")
	if startMonth == "" {
		startMonth = "2018-01"
	}
	endMonth := values.Get("end")
	if endMonth == "" {
		endMonth = "2025-12"
	}
	start, err := time.Parse("2006-01", startMonth)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "start must be YYYY-MM")
		return
	}
	end, err := time.Parse("2006-01", endMonth)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "end must be YYYY-MM")
		return
	}
	if start.After(end) {
		writeError(w, http.StatusUnprocessableEntity, "start must be on or before end")
		return
	}
	bbox, err := query.ParseBBox(values.Get("bbox"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if bbox == nil {
		bbox, _ = query.ParseBBox(s.Config.DefaultBBox)
	}
	country := values.Get("country")

	months := make(map[string]any)
	cursor := start
	count := 0
	for !cursor.After(end) {
		if count >= 120 {
			writeError(w, http.StatusUnprocessableEntity, "archive summary is limited to 120 months")
			return
		}
		month := cursor.Format("2006-01")
		result, err := s.archiveMonthResult(r.Context(), bbox, month)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		months[month] = result.Summary
		cursor = cursor.AddDate(0, 1, 0)
		count++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"country": country,
		"years":   []int{start.Year(), end.Year()},
		"rule":    map[string]any{"maxKm": 1.5, "maxMinutes": 30},
		"months":  months,
	})
}

func (s *Server) handleIntegrationArchiveMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	values := r.URL.Query()
	bbox, err := query.ParseBBox(values.Get("bbox"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if bbox == nil {
		bbox, _ = query.ParseBBox(s.Config.DefaultBBox)
	}
	result, err := s.archiveMonthResult(r.Context(), bbox, month)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	baseTime := time.Now().UTC()
	if !result.Compact.BaseTime.IsZero() {
		baseTime = result.Compact.BaseTime
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"country":     values.Get("country"),
		"month":       result.Month,
		"baseTime":    timeutil.FormatZ(baseTime),
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"rule":        map[string]any{"maxKm": 1.5, "maxMinutes": 30, "widestMinutes": 30},
		"modis":       result.Compact.MODISRows,
		"viirs":       result.Compact.VIIRSRows,
		"links":       result.Compact.Links,
	})
}

func summarizeCompact(compact compactData) map[string]any {
	modisCount := len(compact.MODISRows)
	viirsCount := len(compact.VIIRSRows)
	pairs := len(compact.Links)

	modisBySatellite := map[string]int{"Terra": 0, "Aqua": 0}
	viirsBySatellite := map[string]int{"S-NPP": 0, "NOAA-20": 0, "NOAA-21": 0}
	viirsConfidence := map[string]int{"low": 0, "nominal": 0, "high": 0}
	modisConfidenceBins := make([]int, 10)

	for _, row := range compact.MODISRows {
		values := row.([]any)
		switch intValue(values[5]) {
		case 1:
			modisBySatellite["Aqua"]++
		default:
			modisBySatellite["Terra"]++
		}
		bin := intValue(values[4]) * 10 / 101
		if bin < 0 {
			bin = 0
		}
		if bin > 9 {
			bin = 9
		}
		modisConfidenceBins[bin]++
	}
	for _, row := range compact.VIIRSRows {
		values := row.([]any)
		switch intValue(values[5]) {
		case 1:
			viirsBySatellite["NOAA-20"]++
		case 2:
			viirsBySatellite["NOAA-21"]++
		default:
			viirsBySatellite["S-NPP"]++
		}
		switch intValue(values[4]) {
		case 0:
			viirsConfidence["low"]++
		case 2:
			viirsConfidence["high"]++
		default:
			viirsConfidence["nominal"]++
		}
	}

	linkedMODIS := make(map[int]struct{})
	linkedVIIRS := make(map[int]struct{})
	aquaPaired := make(map[int]struct{})
	terraPaired := make(map[int]struct{})
	frpByMODIS := make(map[int]float64)
	distances := make([]float64, 0, pairs)
	minutes := make([]float64, 0, pairs)
	for _, link := range compact.Links {
		mi := intValue(link[0])
		vi := intValue(link[1])
		linkedMODIS[mi] = struct{}{}
		linkedVIIRS[vi] = struct{}{}
		minutes = append(minutes, floatValue(link[2]))
		if len(link) > 3 {
			distances = append(distances, floatValue(link[3]))
		}
		if mi < len(compact.MODISRows) {
			modis := compact.MODISRows[mi].([]any)
			if intValue(modis[5]) == 1 {
				aquaPaired[mi] = struct{}{}
			} else {
				terraPaired[mi] = struct{}{}
			}
			if vi < len(compact.VIIRSRows) {
				viirs := compact.VIIRSRows[vi].([]any)
				frpByMODIS[mi] += floatValue(viirs[3])
			}
		}
	}

	frpPairs := make([][2]float64, 0, len(frpByMODIS))
	for mi, viirsFRP := range frpByMODIS {
		if mi >= len(compact.MODISRows) {
			continue
		}
		modisFRP := floatValue(compact.MODISRows[mi].([]any)[3])
		frpPairs = append(frpPairs, [2]float64{round1(viirsFRP), round1(modisFRP)})
		if len(frpPairs) >= 900 {
			break
		}
	}

	return map[string]any{
		"modis":               modisCount,
		"viirs":               viirsCount,
		"pairs":               pairs,
		"harmonized":          modisCount + viirsCount - len(linkedVIIRS),
		"modisBySatellite":    modisBySatellite,
		"viirsBySatellite":    viirsBySatellite,
		"aquaPaired":          len(aquaPaired),
		"terraPaired":         len(terraPaired),
		"viirsPaired":         len(linkedVIIRS),
		"medianPairKm":        medianFloat(distances),
		"medianPairMinutes":   medianFloat(minutes),
		"distanceBins":        histogram(distances, 1.5, 15),
		"timeBins":            histogram(minutes, 30, 10),
		"modisConfidenceBins": modisConfidenceBins,
		"viirsConfidence":     viirsConfidence,
		"frp":                 frpPairs,
	}
}

func histogram(values []float64, limit float64, bins int) []int {
	out := make([]int, bins)
	if limit <= 0 {
		return out
	}
	for _, value := range values {
		bin := int(value / limit * float64(bins))
		if bin < 0 {
			bin = 0
		}
		if bin >= bins {
			bin = bins - 1
		}
		out[bin]++
	}
	return out
}

func medianFloat(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	middle := len(copied) / 2
	var value float64
	if len(copied)%2 == 1 {
		value = copied[middle]
	} else {
		value = (copied[middle-1] + copied[middle]) / 2
	}
	return &value
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
