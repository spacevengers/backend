package api

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
)

type exportRequest struct {
	Dataset         string `json:"dataset"`
	Format          string `json:"format"`
	Start           string `json:"start"`
	End             string `json:"end"`
	BBox            string `json:"bbox"`
	Sensor          string `json:"sensor"`
	Satellite       string `json:"satellite"`
	ConfidenceClass string `json:"confidence_class"`
	RunID           *int64 `json:"run_id"`
}

type exportResponse struct {
	models.ExportJob
	DownloadURL string `json:"download_url"`
}

func exportPayload(job models.ExportJob) exportResponse {
	return exportResponse{
		ExportJob:   job,
		DownloadURL: "/api/exports/" + job.ExportUID + "/download",
	}
}

func (s *Server) handleExportCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var payload exportRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.Dataset == "" {
		payload.Dataset = "observations"
	}
	if payload.Format == "" {
		payload.Format = "csv"
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
	observationFilter := query.ObservationFilter{
		Sensor: payload.Sensor, Satellite: payload.Satellite, Start: start, End: end,
		BBox: bbox, ConfidenceClass: payload.ConfidenceClass,
	}
	harmonizedFilter := query.HarmonizedFilter{
		RunID: payload.RunID, Start: start, End: end, BBox: bbox,
	}
	if payload.Dataset == "harmonized" && payload.RunID == nil {
		latest, err := s.Store.LatestCompletedRunID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if latest == nil {
			writeError(w, http.StatusUnprocessableEntity, "run_id is required when exporting harmonized observations")
			return
		}
		harmonizedFilter.RunID = latest
	}
	job, err := s.Exports.Create(
		r.Context(), payload.Dataset, payload.Format, observationFilter, harmonizedFilter)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, exportPayload(*job))
}

func (s *Server) handleExportList(w http.ResponseWriter, r *http.Request) {
	page, err := parsePagination(r, 50, 500)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	jobs, total, err := s.Store.ListExports(r.Context(), page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]exportResponse, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, exportPayload(job))
	}
	writeJSON(w, http.StatusOK, Page[exportResponse]{
		Items: items, Total: total, Limit: page.Limit, Offset: page.Offset,
	})
}

func (s *Server) handleExportDetail(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	job, err := s.Store.GetExportByUID(r.Context(), uid)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, exportPayload(*job))
}

func (s *Server) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	job, err := s.Store.GetExportByUID(r.Context(), uid)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "completed export not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job.Status != "completed" || job.FilePath == nil {
		writeError(w, http.StatusNotFound, "completed export not found")
		return
	}
	path, err := filepath.Abs(*job.FilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	exportDir, err := filepath.Abs(s.Config.ExportDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	relative, err := filepath.Rel(exportDir, path)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		writeError(w, http.StatusNotFound, "export file is unavailable")
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "export file is unavailable")
		return
	}
	mediaType := "text/csv"
	if job.Format == "geojson" {
		mediaType = "application/geo+json"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+job.Dataset+"-"+job.ExportUID+"."+job.Format+`"`)
	http.ServeFile(w, r, path)
}
