package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"firms-harmonizer/internal/models"
)

func (s *Server) handleComparisons(w http.ResponseWriter, r *http.Request) {
	var runID *int64
	if raw := r.URL.Query().Get("run_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "run_id must be a positive integer")
			return
		}
		runID = &parsed
	}

	var run *models.HarmonizationRun
	var err error
	if runID == nil {
		var latest *int64
		latest, err = s.Store.LatestCompletedRunID(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if latest == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"run":       nil,
				"available": false,
				"message":   "No completed harmonization runs yet.",
			})
			return
		}
		run, err = s.Store.GetHarmonizationRun(r.Context(), *latest)
	} else {
		run, err = s.Store.GetHarmonizationRun(r.Context(), *runID)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "harmonization run not found")
		return
	}
	if run.Status != "completed" {
		writeJSON(w, http.StatusOK, map[string]any{
			"run":       run,
			"available": false,
			"message":   "Run status is '" + run.Status + "', not 'completed'.",
		})
		return
	}

	matches, err := s.Store.MatchesForRun(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	distances := make([]float64, 0, len(matches))
	timeDeltas := make([]float64, 0, len(matches))
	frpRatios := make([]float64, 0, len(matches))
	frpDifferences := make([]float64, 0, len(matches))
	type pairAggregate struct {
		count       int
		distanceSum float64
		deltaSum    float64
	}
	pairs := make(map[[2]string]*pairAggregate)

	for _, match := range matches {
		distances = append(distances, match.DistanceKm)
		timeDeltas = append(timeDeltas, match.TimeDeltaMinutes)
		if match.MODIS != nil && match.VIIRS != nil {
			if match.MODIS.FRP != nil && match.VIIRS.FRP != nil {
				frpDifferences = append(frpDifferences, *match.VIIRS.FRP-*match.MODIS.FRP)
				if *match.MODIS.FRP > 0 {
					frpRatios = append(frpRatios, *match.VIIRS.FRP / *match.MODIS.FRP)
				}
			}
			key := [2]string{match.MODIS.Satellite, match.VIIRS.Satellite}
			aggregate, ok := pairs[key]
			if !ok {
				aggregate = &pairAggregate{}
				pairs[key] = aggregate
			}
			aggregate.count++
			aggregate.distanceSum += match.DistanceKm
			aggregate.deltaSum += match.TimeDeltaMinutes
		}
	}

	byPair := make([]map[string]any, 0, len(pairs))
	for key, aggregate := range pairs {
		byPair = append(byPair, map[string]any{
			"modis_satellite":         key[0],
			"viirs_satellite":         key[1],
			"count":                   aggregate.count,
			"mean_distance_km":        round4(aggregate.distanceSum / float64(aggregate.count)),
			"mean_time_delta_minutes": round4(aggregate.deltaSum / float64(aggregate.count)),
		})
	}
	sort.Slice(byPair, func(i, j int) bool {
		left := byPair[i]["modis_satellite"].(string) + byPair[i]["viirs_satellite"].(string)
		right := byPair[j]["modis_satellite"].(string) + byPair[j]["viirs_satellite"].(string)
		return left < right
	})

	var metrics map[string]any
	_ = json.Unmarshal(run.Metrics, &metrics)

	writeJSON(w, http.StatusOK, map[string]any{
		"run":       run,
		"available": true,
		"matches": map[string]any{
			"count":                               len(matches),
			"distance_km":                         summaryMap(distances),
			"time_delta_minutes":                  summaryMap(timeDeltas),
			"frp_ratio_viirs_over_modis":          summaryMap(frpRatios),
			"frp_difference_viirs_minus_modis_mw": summaryMap(frpDifferences),
		},
		"by_satellite_pair": byPair,
		"unmatched": map[string]any{
			"modis": metrics["unmatched_modis_count"],
			"viirs": metrics["unmatched_viirs_count"],
		},
		"clusters_by_composition": metrics["clusters_by_composition"],
		"note": "These are internally computed relationships between potentially " +
			"corresponding observations. They are not ground-truth accuracy estimates.",
	})
}
