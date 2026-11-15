package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"firms-harmonizer/internal/config"
	"firms-harmonizer/internal/export"
	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
	"firms-harmonizer/internal/timeutil"
)

type Server struct {
	Config  config.Config
	Store   *store.Store
	Exports *export.Service
	Client  *firms.Client
}

func New(cfg config.Config, repository *store.Store) *Server {
	return &Server{
		Config: cfg,
		Store:  repository,
		Exports: &export.Service{
			Store:     repository,
			ExportDir: cfg.ExportDir,
			MaxRows:   cfg.MaxExportRows,
		},
		Client: firms.NewClient(
			cfg.FirmsMapKey, cfg.FirmsBaseURL, cfg.FirmsTimeout,
			cfg.FirmsMaxRetries, cfg.FirmsMaxDays, cfg.FirmsRequestDelay,
		),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("GET /api/observations", s.handleObservationList)
	mux.HandleFunc("GET /api/observations/geojson", s.handleObservationGeoJSON)
	mux.HandleFunc("GET /api/observations/{id}", s.handleObservationDetail)

	mux.HandleFunc("GET /api/harmonized-observations", s.handleHarmonizedList)
	mux.HandleFunc("GET /api/harmonized-observations/{id}", s.handleHarmonizedDetail)

	mux.HandleFunc("POST /api/ingestion/runs", s.handleIngestionQueue)
	mux.HandleFunc("GET /api/ingestion/runs", s.handleIngestionList)
	mux.HandleFunc("GET /api/ingestion/runs/{id}", s.handleIngestionDetail)

	mux.HandleFunc("POST /api/harmonization-runs", s.handleHarmonizationQueue)
	mux.HandleFunc("GET /api/harmonization-runs", s.handleHarmonizationList)
	mux.HandleFunc("GET /api/harmonization-runs/{id}", s.handleHarmonizationDetail)
	mux.HandleFunc("POST /api/harmonization-runs/{id}/execute", s.handleHarmonizationRetry)

	mux.HandleFunc("GET /api/statistics", s.handleStatistics)
	mux.HandleFunc("GET /api/comparisons", s.handleComparisons)
	mux.HandleFunc("GET /api/metadata", s.handleMetadata)

	mux.HandleFunc("POST /api/exports", s.handleExportCreate)
	mux.HandleFunc("GET /api/exports", s.handleExportList)
	mux.HandleFunc("GET /api/exports/{uid}", s.handleExportDetail)
	mux.HandleFunc("GET /api/exports/{uid}/download", s.handleExportDownload)

	return s.withCORS(s.withRecovery(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":         "ok",
		"schema_version": s.Config.SchemaVersion,
	})
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(s.Config.CORSOrigins))
	for _, origin := range s.Config.CORSOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Admin-Token")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("internal error: %v", recovered))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.Config.AdminToken == "" {
		return true
	}
	if r.Header.Get("X-Admin-Token") != s.Config.AdminToken {
		writeError(w, http.StatusUnauthorized, "valid X-Admin-Token required")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"detail": message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

type pagination struct {
	Limit  int
	Offset int
	SortBy string
	Order  string
}

func parsePagination(r *http.Request, defaultLimit, maxLimit int) (pagination, error) {
	values := r.URL.Query()
	page := pagination{Limit: defaultLimit, Offset: 0, SortBy: "acquired_at", Order: "desc"}
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxLimit {
			return page, fmt.Errorf("limit must be between 1 and %d", maxLimit)
		}
		page.Limit = parsed
	}
	if raw := values.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return page, fmt.Errorf("offset must be a non-negative integer")
		}
		page.Offset = parsed
	}
	if raw := values.Get("sort_by"); raw != "" {
		switch raw {
		case "acquired_at", "frp", "confidence_numeric":
			page.SortBy = raw
		default:
			return page, fmt.Errorf("unsupported sort_by %q", raw)
		}
	}
	if raw := values.Get("order"); raw != "" {
		if raw != "asc" && raw != "desc" {
			return page, fmt.Errorf("order must be 'asc' or 'desc'")
		}
		page.Order = raw
	}
	return page, nil
}

func parseObservationFilter(r *http.Request) (query.ObservationFilter, error) {
	values := r.URL.Query()
	filter := query.ObservationFilter{
		Sensor:          values.Get("sensor"),
		Satellite:       values.Get("satellite"),
		Source:          values.Get("source"),
		Processing:      values.Get("processing"),
		ConfidenceClass: values.Get("confidence_class"),
		DayNight:        values.Get("daynight"),
	}
	if filter.Sensor != "" && filter.Sensor != "MODIS" && filter.Sensor != "VIIRS" {
		return filter, fmt.Errorf("sensor must be MODIS or VIIRS")
	}
	if filter.Processing != "" && filter.Processing != "SP" && filter.Processing != "NRT" {
		return filter, fmt.Errorf("processing must be SP or NRT")
	}
	if filter.ConfidenceClass != "" {
		switch filter.ConfidenceClass {
		case "low", "nominal", "high":
		default:
			return filter, fmt.Errorf("confidence_class must be low, nominal, or high")
		}
	}
	if filter.DayNight != "" && filter.DayNight != "D" && filter.DayNight != "N" {
		return filter, fmt.Errorf("daynight must be D or N")
	}
	if raw := values.Get("start"); raw != "" {
		parsed, err := timeutil.ParseBound(raw, false)
		if err != nil {
			return filter, err
		}
		filter.Start = &parsed
	}
	if raw := values.Get("end"); raw != "" {
		parsed, err := timeutil.ParseBound(raw, true)
		if err != nil {
			return filter, err
		}
		filter.End = &parsed
	}
	bbox, err := query.ParseBBox(values.Get("bbox"))
	if err != nil {
		return filter, err
	}
	filter.BBox = bbox
	if filter.MinConfidence, err = optionalFloatQuery(values, "min_confidence"); err != nil {
		return filter, err
	}
	if filter.MaxConfidence, err = optionalFloatQuery(values, "max_confidence"); err != nil {
		return filter, err
	}
	if filter.MinFRP, err = optionalFloatQuery(values, "min_frp"); err != nil {
		return filter, err
	}
	if filter.MaxFRP, err = optionalFloatQuery(values, "max_frp"); err != nil {
		return filter, err
	}
	if filter.MinConfidence != nil && filter.MaxConfidence != nil &&
		*filter.MinConfidence > *filter.MaxConfidence {
		return filter, fmt.Errorf("min_confidence cannot exceed max_confidence")
	}
	if filter.MinFRP != nil && filter.MaxFRP != nil && *filter.MinFRP > *filter.MaxFRP {
		return filter, fmt.Errorf("min_frp cannot exceed max_frp")
	}
	return filter, nil
}

func optionalFloatQuery(values map[string][]string, name string) (*float64, error) {
	raw := ""
	if list, ok := values[name]; ok && len(list) > 0 {
		raw = list[0]
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be numeric", name)
	}
	return &parsed, nil
}

func parsePathID(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func parseDateRange(startRaw, endRaw string) (*time.Time, *time.Time, error) {
	var start, end *time.Time
	if startRaw != "" {
		parsed, err := timeutil.ParseBound(startRaw, false)
		if err != nil {
			return nil, nil, err
		}
		start = &parsed
	}
	if endRaw != "" {
		parsed, err := timeutil.ParseBound(endRaw, true)
		if err != nil {
			return nil, nil, err
		}
		end = &parsed
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, nil, fmt.Errorf("start must be on or before end")
	}
	return start, end, nil
}
