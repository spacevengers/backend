package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/ingest"
	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/timeutil"
)

type ingestionRequest struct {
	Source    string `json:"source"`
	BBox      string `json:"bbox"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Trigger   string `json:"trigger"`
}

func (s *Server) handleIngestionQueue(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var payload ingestionRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Config.FirmsMapKey == "" {
		writeError(w, http.StatusBadRequest, "FIRMS_MAP_KEY is not configured on the server")
		return
	}
	product, err := firms.GetProduct(payload.Source)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	bbox := payload.BBox
	if bbox == "" {
		bbox = s.Config.DefaultBBox
	}
	if _, err := query.ParseBBox(bbox); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	startDate, err := timeutil.ParseBound(payload.StartDate, false)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	endDate, err := timeutil.ParseBound(payload.EndDate, true)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if startDate.After(endDate) {
		writeError(w, http.StatusUnprocessableEntity, "start_date must be on or before end_date")
		return
	}
	trigger := payload.Trigger
	if trigger == "" {
		trigger = "api"
	}
	run, err := ingest.CreateRun(r.Context(), s.Store, product, bbox, startDate, endDate, trigger)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Config.SynchronousJobs {
		run, err = ingest.IngestProduct(
			r.Context(), s.Store, run, product, bbox, startDate, endDate, s.Client)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, run)
			return
		}
	} else {
		go func(runID int64) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
			defer cancel()
			_ = ingest.ExecuteRun(ctx, s.Store, runID, s.Client)
		}(run.ID)
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleIngestionList(w http.ResponseWriter, r *http.Request) {
	page, err := parsePagination(r, 50, 500)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	values := r.URL.Query()
	runs, total, err := s.Store.ListIngestionRuns(
		r.Context(), values.Get("status"), values.Get("source"), page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for index := range runs {
		runs[index].Errors = nil
	}
	writeJSON(w, http.StatusOK, Page[models.IngestionRun]{
		Items: runs, Total: total, Limit: page.Limit, Offset: page.Offset,
	})
}

func (s *Server) handleIngestionDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ingestion run id")
		return
	}
	run, err := s.Store.GetIngestionRun(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "ingestion run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}
