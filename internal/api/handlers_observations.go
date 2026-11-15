package api

import (
	"database/sql"
	"errors"
	"net/http"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/timeutil"
)

func (s *Server) handleObservationList(w http.ResponseWriter, r *http.Request) {
	filter, err := parseObservationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	page, err := parsePagination(r, 500, 5000)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	total, err := s.Store.CountObservations(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := s.Store.ListObservations(
		r.Context(), filter, page.SortBy, page.Order, page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for index := range rows {
		rows[index].Raw = nil
	}
	writeJSON(w, http.StatusOK, Page[models.Observation]{
		Items: rows, Total: total, Limit: page.Limit, Offset: page.Offset,
	})
}

func (s *Server) handleObservationGeoJSON(w http.ResponseWriter, r *http.Request) {
	filter, err := parseObservationFilter(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	page, err := parsePagination(r, 500, 5000)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	total, err := s.Store.CountObservations(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := s.Store.ListObservations(
		r.Context(), filter, page.SortBy, page.Order, page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	features := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		features = append(features, map[string]any{
			"type": "Feature",
			"id":   row.ID,
			"geometry": map[string]any{
				"type":        "Point",
				"coordinates": []float64{row.Longitude, row.Latitude},
			},
			"properties": map[string]any{
				"observation_uid":    row.ObservationUID,
				"sensor":             row.Sensor,
				"satellite":          row.Satellite,
				"source":             row.Source,
				"source_product":     row.SourceProduct,
				"processing":         row.Processing,
				"acquired_at":        timeutil.FormatZ(row.AcquiredAt),
				"confidence_raw":     row.ConfidenceRaw,
				"confidence_numeric": row.ConfidenceNumeric,
				"confidence_class":   row.ConfidenceClass,
				"confidence_scheme":  row.ConfidenceScheme,
				"frp":                row.FRP,
				"frp_units":          row.FRPUnits,
				"daynight":           row.DayNight,
				"fire_type":          row.FireType,
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type":     "FeatureCollection",
		"features": features,
		"meta": map[string]any{
			"total":     total,
			"returned":  len(features),
			"limit":     page.Limit,
			"offset":    page.Offset,
			"truncated": total > page.Offset+len(features),
		},
	})
}

func (s *Server) handleObservationDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid observation id")
		return
	}
	observation, err := s.Store.GetObservation(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "observation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, observation)
}
