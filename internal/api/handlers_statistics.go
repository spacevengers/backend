package api

import (
	"math"
	"net/http"
	"sort"
	"time"
)

type sensorStat struct {
	Sensor            string         `json:"sensor"`
	Count             int            `json:"count"`
	FRP               map[string]any `json:"frp"`
	ConfidenceClasses map[string]int `json:"confidence_classes"`
}

type dateStat struct {
	Date  string `json:"date"`
	MODIS int    `json:"modis"`
	VIIRS int    `json:"viirs"`
	Total int    `json:"total"`
}

func (s *Server) handleStatistics(w http.ResponseWriter, r *http.Request) {
	filter, err := parseObservationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	total, err := s.Store.CountObservations(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sensorCounts, err := s.Store.CountsBySensor(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	satelliteCounts, err := s.Store.CountsBySatellite(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dateCounts, err := s.Store.CountsByDate(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	confidenceCounts, err := s.Store.CountsByConfidence(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	frpValues, err := s.Store.FRPValuesBySensor(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coverage, err := s.Store.Coverage(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statsBySensor := make(map[string]*sensorStat)
	for _, item := range sensorCounts {
		statsBySensor[item.Sensor] = &sensorStat{
			Sensor:            item.Sensor,
			Count:             item.Count,
			FRP:               summaryMap(frpValues[item.Sensor]),
			ConfidenceClasses: map[string]int{},
		}
	}
	for _, item := range confidenceCounts {
		if stat, ok := statsBySensor[item.Sensor]; ok {
			stat.ConfidenceClasses[item.Class] = item.Count
		}
	}
	bySensor := make([]sensorStat, 0, len(statsBySensor))
	for _, stat := range statsBySensor {
		bySensor = append(bySensor, *stat)
	}
	sort.Slice(bySensor, func(i, j int) bool { return bySensor[i].Sensor < bySensor[j].Sensor })

	statsByDate := make(map[string]*dateStat)
	for _, item := range dateCounts {
		entry, ok := statsByDate[item.Date]
		if !ok {
			entry = &dateStat{Date: item.Date}
			statsByDate[item.Date] = entry
		}
		switch item.Sensor {
		case "MODIS":
			entry.MODIS = item.Count
		case "VIIRS":
			entry.VIIRS = item.Count
		}
		entry.Total += item.Count
	}
	byDate := make([]dateStat, 0, len(statsByDate))
	for _, entry := range statsByDate {
		byDate = append(byDate, *entry)
	}
	sort.Slice(byDate, func(i, j int) bool { return byDate[i].Date < byDate[j].Date })

	confidenceOut := make([]map[string]any, 0, len(statsBySensor))
	for _, stat := range bySensor {
		confidenceOut = append(confidenceOut, map[string]any{
			"sensor":  stat.Sensor,
			"low":     stat.ConfidenceClasses["low"],
			"nominal": stat.ConfidenceClasses["nominal"],
			"high":    stat.ConfidenceClasses["high"],
			"unknown": stat.ConfidenceClasses["unknown"],
		})
	}

	modis, viirs := 0, 0
	for _, item := range sensorCounts {
		switch item.Sensor {
		case "MODIS":
			modis = item.Count
		case "VIIRS":
			viirs = item.Count
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"filters":      filter,
		"totals": map[string]int{
			"observations": total,
			"modis":        modis,
			"viirs":        viirs,
		},
		"coverage":     coverage,
		"by_sensor":    bySensor,
		"by_satellite": satelliteCounts,
		"by_date":      byDate,
		"confidence":   confidenceOut,
	})
}

func summaryMap(values []float64) map[string]any {
	if len(values) == 0 {
		return map[string]any{
			"count": 0, "min": nil, "p50": nil, "mean": nil, "p95": nil, "max": nil,
		}
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	sum := 0.0
	for _, value := range ordered {
		sum += value
	}
	return map[string]any{
		"count": len(ordered),
		"min":   round4(ordered[0]),
		"p50":   round4(percentileValue(ordered, 0.5)),
		"mean":  round4(sum / float64(len(ordered))),
		"p95":   round4(percentileValue(ordered, 0.95)),
		"max":   round4(ordered[len(ordered)-1]),
	}
}

func percentileValue(ordered []float64, fraction float64) float64 {
	if len(ordered) == 1 {
		return ordered[0]
	}
	position := float64(len(ordered)-1) * fraction
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return ordered[lower]
	}
	return ordered[lower] + (ordered[upper]-ordered[lower])*(position-float64(lower))
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}
