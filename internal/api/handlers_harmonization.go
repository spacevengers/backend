package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"firms-harmonizer/internal/harmonize"
	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
)

type harmonizationRequest struct {
	Method         string   `json:"method"`
	Start          string   `json:"start"`
	End            string   `json:"end"`
	BBox           string   `json:"bbox"`
	Sensor         string   `json:"sensor"`
	MaxDistanceKm  *float64 `json:"max_distance_km"`
	MaxTimeMinutes *float64 `json:"max_time_minutes"`
}

func (s *Server) handleHarmonizationQueue(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var payload harmonizationRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	method := strings.ToLower(payload.Method)
	if method == "" {
		method = s.Config.HarmonizerMethod
	}
	if method != "baseline" && method != "remote" {
		writeError(w, http.StatusUnprocessableEntity, "method must be 'baseline' or 'remote'")
		return
	}
	start, end, err := parseDateRange(payload.Start, payload.End)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	bbox, err := query.ParseBBox(payload.BBox)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if payload.Sensor != "" && payload.Sensor != "MODIS" && payload.Sensor != "VIIRS" {
		writeError(w, http.StatusUnprocessableEntity, "sensor must be MODIS or VIIRS")
		return
	}
	params := harmonize.Params{
		MaxDistanceKm:  s.Config.HarmonizeMaxKm,
		MaxTimeMinutes: s.Config.HarmonizeMaxMin,
	}
	if payload.MaxDistanceKm != nil {
		if *payload.MaxDistanceKm <= 0 || *payload.MaxDistanceKm > 100 {
			writeError(w, http.StatusUnprocessableEntity, "max_distance_km must be in (0, 100]")
			return
		}
		params.MaxDistanceKm = *payload.MaxDistanceKm
	}
	if payload.MaxTimeMinutes != nil {
		if *payload.MaxTimeMinutes <= 0 || *payload.MaxTimeMinutes > 10080 {
			writeError(w, http.StatusUnprocessableEntity, "max_time_minutes must be in (0, 10080]")
			return
		}
		params.MaxTimeMinutes = *payload.MaxTimeMinutes
	}
	filter := query.ObservationFilter{
		Sensor: payload.Sensor, Start: start, End: end, BBox: bbox,
	}
	run, err := harmonize.CreateRun(r.Context(), s.Store, method, filter, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Config.SynchronousJobs {
		run, err = harmonize.Execute(
			r.Context(), s.Store, run.ID, s.Config.HarmonizerEndpoint, s.Config.HarmonizerTimeout)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, run)
			return
		}
	} else {
		go func(runID int64) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			_, _ = harmonize.Execute(ctx, s.Store, runID,
				s.Config.HarmonizerEndpoint, s.Config.HarmonizerTimeout)
		}(run.ID)
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleHarmonizationList(w http.ResponseWriter, r *http.Request) {
	page, err := parsePagination(r, 50, 500)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	runs, total, err := s.Store.ListHarmonizationRuns(
		r.Context(), r.URL.Query().Get("status"), page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Page[models.HarmonizationRun]{
		Items: runs, Total: total, Limit: page.Limit, Offset: page.Offset,
	})
}

func (s *Server) handleHarmonizationDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid harmonization run id")
		return
	}
	run, err := s.Store.GetHarmonizationRun(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "harmonization run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type predictionImportRequest struct {
	Model       string             `json:"model"`
	Predictions []store.Prediction `json:"predictions"`
}

func (s *Server) handlePredictionImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid harmonization run id")
		return
	}
	var payload predictionImportRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(payload.Predictions) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "predictions must not be empty")
		return
	}
	run, err := s.Store.GetHarmonizationRun(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "harmonization run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run.Status != "completed" {
		writeError(w, http.StatusConflict, "predictions require a completed harmonization run")
		return
	}
	updated, err := s.Store.UpdatePredictions(r.Context(), run.ID, payload.Model, payload.Predictions)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id": run.ID, "model": payload.Model, "updated": updated,
	})
}

func (s *Server) handleHarmonizationRetry(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid harmonization run id")
		return
	}
	run, err := s.Store.GetHarmonizationRun(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "harmonization run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if run.Status != "pending" && run.Status != "failed" {
		writeError(w, http.StatusConflict, "run is not retryable")
		return
	}
	run.Status = "pending"
	run.Error = nil
	if err := s.Store.UpdateHarmonizationRun(r.Context(), run); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Config.SynchronousJobs {
		run, err = harmonize.Execute(
			r.Context(), s.Store, run.ID, s.Config.HarmonizerEndpoint, s.Config.HarmonizerTimeout)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, run)
			return
		}
	} else {
		go func(runID int64) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			_, _ = harmonize.Execute(ctx, s.Store, runID,
				s.Config.HarmonizerEndpoint, s.Config.HarmonizerTimeout)
		}(run.ID)
	}
	writeJSON(w, http.StatusAccepted, run)
}
