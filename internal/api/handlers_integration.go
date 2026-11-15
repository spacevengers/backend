package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"firms-harmonizer/internal/harmonize"
	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/timeutil"
)

// The /api/integration endpoints are view adapters for the TerraWatch
// frontend. They intentionally do not replace the native API.

type frontendObservation struct {
	ObservationID string  `json:"observation_id"`
	Sensor        string  `json:"sensor"`
	Satellite     string  `json:"satellite"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	AcquiredAt    string  `json:"acquired_at"`
	Confidence    any     `json:"confidence"`
	FRP           float64 `json:"frp"`
	DayNight      string  `json:"daynight"`
	SourceProduct string  `json:"source_product"`
}

type frontendHarmonized struct {
	HarmonizedID    string   `json:"harmonized_id"`
	ClusterUID      string   `json:"cluster_uid"`
	RunID           int64    `json:"run_id"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	AcquiredAt      string   `json:"acquired_at"`
	MatchStatus     string   `json:"match_status"`
	MemberIDs       []string `json:"member_ids"`
	NVIIRS          int      `json:"n_viirs"`
	NModis          int      `json:"n_modis"`
	FRPVIIRSSum     *float64 `json:"frp_viirs_sum"`
	FRPModis        *float64 `json:"frp_modis"`
	PModisDetect    *float64 `json:"p_modis_detect"`
	FRPCalibrated   *float64 `json:"frp_calibrated"`
	PredictionModel *string  `json:"prediction_model"`
}

func integrationFilter(r *http.Request) (query.ObservationFilter, error) {
	values := r.URL.Query()
	if len(values["sensor"]) > 1 {
		values.Del("sensor")
	}
	clone := r.Clone(r.Context())
	clone.URL.RawQuery = values.Encode()
	return parseObservationFilter(clone)
}

func (s *Server) handleIntegrationObservations(w http.ResponseWriter, r *http.Request) {
	filter, err := integrationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	limit := 50000
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= limit {
			limit = parsed
		}
	}
	rows, err := s.Store.ListObservations(r.Context(), filter, "acquired_at", "asc", limit, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]frontendObservation, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapFrontendObservation(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleIntegrationHarmonized(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	var runID *int64
	if raw := values.Get("run_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "run_id must be a positive integer")
			return
		}
		runID = &parsed
	} else {
		latest, err := s.Store.LatestCompletedRunID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		runID = latest
	}
	if runID == nil {
		writeJSON(w, http.StatusOK, []frontendHarmonized{})
		return
	}
	start, end, err := parseDateRange(values.Get("start"), values.Get("end"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	bbox, err := query.ParseBBox(values.Get("bbox"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	limit := 5000
	if raw := values.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 50000 {
			limit = parsed
		}
	}
	filter := query.HarmonizedFilter{RunID: runID, Start: start, End: end, BBox: bbox}
	clusters, err := s.Store.ListHarmonized(r.Context(), filter, limit, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := make([]int64, 0, len(clusters))
	for _, cluster := range clusters {
		ids = append(ids, cluster.ID)
	}
	aggregates, err := s.Store.MemberAggregates(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]frontendHarmonized, 0, len(clusters))
	for _, cluster := range clusters {
		aggregate := aggregates[cluster.ID]
		memberIDs := make([]string, 0, len(aggregate.MemberIDs))
		for _, id := range aggregate.MemberIDs {
			memberIDs = append(memberIDs, strconv.FormatInt(id, 10))
		}
		out = append(out, frontendHarmonized{
			HarmonizedID:    strconv.FormatInt(cluster.ID, 10),
			ClusterUID:      cluster.ClusterUID,
			RunID:           cluster.RunID,
			Latitude:        cluster.CentroidLatitude,
			Longitude:       cluster.CentroidLongitude,
			AcquiredAt:      timeutil.FormatZ(cluster.StartTime),
			MatchStatus:     matchStatus(cluster),
			MemberIDs:       memberIDs,
			NVIIRS:          cluster.VIIRSCount,
			NModis:          cluster.MODISCount,
			FRPVIIRSSum:     aggregate.FRPVIIRS,
			FRPModis:        aggregate.FRPMODIS,
			PModisDetect:    cluster.PModisDetect,
			FRPCalibrated:   cluster.FRPCalibrated,
			PredictionModel: cluster.PredictionModel,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleIntegrationStatistics(w http.ResponseWriter, r *http.Request) {
	filter, err := integrationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	counts, err := s.Store.CountsBySensor(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	observationCounts := map[string]int{"MODIS": 0, "VIIRS": 0}
	for _, item := range counts {
		observationCounts[item.Sensor] = item.Count
	}
	harmonizedCounts := map[string]int{"matched": 0, "modis_only": 0, "viirs_only": 0}

	values := r.URL.Query()
	var runID *int64
	if raw := values.Get("run_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "run_id must be a positive integer")
			return
		}
		runID = &parsed
	} else {
		runID, err = s.Store.LatestCompletedRunID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if runID != nil {
		harmonizedCounts, err = s.Store.CountHarmonizedByStatus(r.Context(), query.HarmonizedFilter{
			RunID: runID, Start: filter.Start, End: filter.End, BBox: filter.BBox,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"counts":     observationCounts,
		"harmonized": harmonizedCounts,
	})
}

func (s *Server) handleIntegrationFirms(w http.ResponseWriter, r *http.Request) {
	filter, err := integrationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	values := r.URL.Query()
	maxDistance := 1.5
	if parsed, err := strconv.ParseFloat(values.Get("max_distance_km"), 64); err == nil && parsed > 0 {
		maxDistance = parsed
	}
	pairMinutes := 30.0
	if parsed, err := strconv.ParseFloat(values.Get("pair_minutes"), 64); err == nil && parsed > 0 {
		pairMinutes = parsed
	}
	widestMinutes := 180.0
	if parsed, err := strconv.ParseFloat(values.Get("max_minutes"), 64); err == nil && parsed > 0 {
		widestMinutes = parsed
	}
	if pairMinutes > widestMinutes {
		writeError(w, http.StatusUnprocessableEntity, "pair_minutes cannot exceed max_minutes")
		return
	}

	total, err := s.Store.CountObservations(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if total > 100000 {
		writeError(w, http.StatusRequestEntityTooLarge,
			"more than 100,000 observations match; narrow the date range or bbox")
		return
	}
	rows, err := s.Store.ListObservations(r.Context(), filter, "acquired_at", "asc", 100000, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"generatedAt": time.Now().UTC().Format(time.RFC3339),
			"baseTime":    time.Now().UTC().Format(time.RFC3339),
			"rule": map[string]any{
				"maxKm": maxDistance, "maxMinutes": pairMinutes, "widestMinutes": widestMinutes,
			},
			"sources": []any{}, "modis": []any{}, "viirs": []any{}, "links": []any{},
		})
		return
	}

	base := rows[0].AcquiredAt
	contract := make([]harmonize.Observation, 0, len(rows))
	for _, row := range rows {
		if row.AcquiredAt.Before(base) {
			base = row.AcquiredAt
		}
		contract = append(contract, harmonize.Observation{
			ObservationID:     row.ID,
			ObservationUID:    row.ObservationUID,
			Sensor:            row.Sensor,
			Satellite:         row.Satellite,
			Latitude:          row.Latitude,
			Longitude:         row.Longitude,
			AcquiredAt:        row.AcquiredAt,
			ConfidenceRaw:     row.ConfidenceRaw,
			ConfidenceNumeric: row.ConfidenceNumeric,
			ConfidenceClass:   row.ConfidenceClass,
			ConfidenceScheme:  row.ConfidenceScheme,
			FRP:               row.FRP,
			FRPUnits:          row.FRPUnits,
		})
	}
	matched := harmonize.BaselineHarmonize(contract, harmonize.Params{
		MaxDistanceKm: maxDistance, MaxTimeMinutes: widestMinutes,
	}, "integration")

	modisIndex := make(map[int64]int)
	viirsIndex := make(map[int64]int)
	partnerCount := make(map[int64]int)
	nearestKm := make(map[int64]float64)
	nearestMinutes := make(map[int64]float64)
	viirsNearest := make(map[int64]float64)
	for _, row := range rows {
		switch row.Sensor {
		case "MODIS":
			modisIndex[row.ID] = len(modisIndex)
		case "VIIRS":
			viirsIndex[row.ID] = len(viirsIndex)
		}
	}
	for _, match := range matched.Matches {
		partnerCount[match.MODISObservationID]++
		if current, ok := nearestKm[match.MODISObservationID]; !ok || match.DistanceKm < current {
			nearestKm[match.MODISObservationID] = match.DistanceKm
			nearestMinutes[match.MODISObservationID] = match.TimeDeltaMinutes
		}
		if current, ok := viirsNearest[match.VIIRSObservationID]; !ok || match.TimeDeltaMinutes < current {
			viirsNearest[match.VIIRSObservationID] = match.TimeDeltaMinutes
		}
	}

	modisRows := make([]any, 0, len(modisIndex))
	viirsRows := make([]any, 0, len(viirsIndex))
	for _, row := range rows {
		minutes := int(math.Round(row.AcquiredAt.Sub(base).Minutes()))
		if row.Sensor == "MODIS" {
			nearestDistance := -1.0
			nearest := -1.0
			if value, ok := nearestKm[row.ID]; ok {
				nearestDistance = value
				nearest = nearestMinutes[row.ID]
			}
			modisRows = append(modisRows, []any{
				round5(row.Longitude), round5(row.Latitude), minutes, round1(valueOrZero(row.FRP)),
				modisConfidence(row), modisSatelliteIndex(row.Satellite), partnerCount[row.ID],
				round2(nearestDistance), nearest, valueOrZeroInt(row.FireType),
			})
		} else {
			nearest := -1.0
			if value, ok := viirsNearest[row.ID]; ok {
				nearest = value
			}
			viirsRows = append(viirsRows, []any{
				round5(row.Longitude), round5(row.Latitude), minutes, round1(valueOrZero(row.FRP)),
				viirsConfidenceIndex(row), viirsSatelliteIndex(row.Satellite), nearest,
				valueOrZeroInt(row.FireType),
			})
		}
	}

	links := make([][]any, 0, len(matched.Matches))
	for _, match := range matched.Matches {
		links = append(links, []any{
			modisIndex[match.MODISObservationID], viirsIndex[match.VIIRSObservationID],
			match.TimeDeltaMinutes,
		})
	}

	sourceCounts, _ := s.Store.CountsBySource(r.Context(), filter)
	sources := make([]any, 0, len(sourceCounts))
	for _, source := range sourceCounts {
		sources = append(sources, map[string]any{
			"name": source.Source, "url": "", "rows": source.Count,
			"updated": nil, "ok": true,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"baseTime":    timeutil.FormatZ(base),
		"rule": map[string]any{
			"maxKm": maxDistance, "maxMinutes": pairMinutes, "widestMinutes": widestMinutes,
		},
		"sources": sources,
		"modis":   modisRows,
		"viirs":   viirsRows,
		"links":   links,
	})
}

func mapFrontendObservation(row models.Observation) frontendObservation {
	return frontendObservation{
		ObservationID: strconv.FormatInt(row.ID, 10),
		Sensor:        row.Sensor,
		Satellite:     frontendSatellite(row.Satellite),
		Latitude:      row.Latitude,
		Longitude:     row.Longitude,
		AcquiredAt:    timeutil.FormatZ(row.AcquiredAt),
		Confidence:    frontendConfidence(row),
		FRP:           valueOrZero(row.FRP),
		DayNight:      stringValueOrEmpty(row.DayNight),
		SourceProduct: row.Source,
	}
}

func frontendSatellite(value string) string {
	switch value {
	case "Suomi NPP":
		return "S-NPP"
	default:
		return value
	}
}

func frontendConfidence(row models.Observation) any {
	if row.ConfidenceNumeric != nil {
		return *row.ConfidenceNumeric
	}
	if row.Sensor == "VIIRS" {
		if row.ConfidenceRaw != nil {
			raw := *row.ConfidenceRaw
			if raw == "l" || raw == "n" || raw == "h" {
				return raw
			}
		}
		if row.ConfidenceClass != nil {
			switch *row.ConfidenceClass {
			case "low":
				return "l"
			case "nominal":
				return "n"
			case "high":
				return "h"
			}
		}
		return "n"
	}
	if row.ConfidenceClass != nil {
		switch *row.ConfidenceClass {
		case "low":
			return 25
		case "nominal":
			return 60
		case "high":
			return 90
		}
	}
	return 0
}

func matchStatus(cluster models.HarmonizedObservation) string {
	if cluster.MODISCount > 0 && cluster.VIIRSCount > 0 {
		return "matched"
	}
	if cluster.MODISCount > 0 {
		return "modis_only"
	}
	return "viirs_only"
}

func modisConfidence(row models.Observation) float64 {
	if row.ConfidenceNumeric != nil {
		return *row.ConfidenceNumeric
	}
	if row.ConfidenceClass != nil {
		switch *row.ConfidenceClass {
		case "low":
			return 25
		case "nominal":
			return 60
		case "high":
			return 90
		}
	}
	return 0
}

func modisSatelliteIndex(value string) int {
	if value == "Aqua" {
		return 1
	}
	return 0
}

func viirsConfidenceIndex(row models.Observation) int {
	if row.ConfidenceNumeric != nil {
		switch {
		case *row.ConfidenceNumeric < 30:
			return 0
		case *row.ConfidenceNumeric < 80:
			return 1
		default:
			return 2
		}
	}
	if row.ConfidenceClass != nil {
		switch *row.ConfidenceClass {
		case "low":
			return 0
		case "high":
			return 2
		}
	}
	return 1
}

func viirsSatelliteIndex(value string) int {
	switch value {
	case "NOAA-20":
		return 1
	case "NOAA-21":
		return 2
	default:
		return 0
	}
}

func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func valueOrZeroInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func stringValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func round1(value float64) float64 { return math.Round(value*10) / 10 }
func round2(value float64) float64 { return math.Round(value*100) / 100 }
func round5(value float64) float64 { return math.Round(value*100000) / 100000 }
