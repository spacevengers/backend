package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
)

func (s *Server) handleHarmonizedList(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	page, err := parsePagination(r, 500, 5000)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
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
		if latest == nil {
			writeJSON(w, http.StatusOK, Page[models.HarmonizedObservation]{
				Items: []models.HarmonizedObservation{}, Total: 0,
				Limit: page.Limit, Offset: page.Offset,
			})
			return
		}
		runID = latest
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
	minFRP, err := optionalFloatQuery(values, "min_frp")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	maxFRP, err := optionalFloatQuery(values, "max_frp")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	includeMembers := values.Get("include_members") == "true" || values.Get("include_members") == "1"
	composedOnly := values.Get("composed_only") == "true" || values.Get("composed_only") == "1"

	filter := query.HarmonizedFilter{
		RunID:        runID,
		Start:        start,
		End:          end,
		BBox:         bbox,
		MinFRP:       minFRP,
		MaxFRP:       maxFRP,
		ComposedOnly: composedOnly,
	}
	total, err := s.Store.CountHarmonized(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := s.Store.ListHarmonized(r.Context(), filter, page.Limit, page.Offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if includeMembers {
		for index := range rows {
			full, err := s.Store.GetHarmonized(r.Context(), rows[index].ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rows[index].Members = full.Members
		}
	}
	writeJSON(w, http.StatusOK, Page[models.HarmonizedObservation]{
		Items: rows, Total: total, Limit: page.Limit, Offset: page.Offset,
	})
}

func (s *Server) handleHarmonizedDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid harmonized observation id")
		return
	}
	cluster, err := s.Store.GetHarmonized(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "harmonized observation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}
