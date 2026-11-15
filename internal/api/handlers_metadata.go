package api

import (
	"net/http"
	"time"

	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/query"
)

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	coverage, err := s.Store.Coverage(r.Context(), query.ObservationFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	products, err := s.Store.ProductCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ingestionRuns, _, err := s.Store.ListIngestionRuns(r.Context(), "", "", 10, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	harmonizationRuns, _, err := s.Store.ListHarmonizationRuns(r.Context(), "", 10, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for index := range ingestionRuns {
		ingestionRuns[index].Errors = nil
	}

	sources := make([]map[string]string, 0)
	for _, product := range firms.AllProducts() {
		sources = append(sources, map[string]string{
			"source":      product.Source,
			"sensor":      product.Sensor,
			"product":     product.Product,
			"processing":  product.Processing,
			"description": product.Description,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version": s.Config.SchemaVersion,
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"study_defaults": map[string]any{
			"bbox":       s.Config.DefaultBBox,
			"bbox_order": "west,south,east,north",
			"note":       "Kenya is the proposed initial study area; confirm before production runs.",
		},
		"coverage":                  coverage,
		"products":                  products,
		"available_sources":         sources,
		"recent_ingestion_runs":     ingestionRuns,
		"recent_harmonization_runs": harmonizationRuns,
		"semantics": map[string]string{
			"confidence": "Raw confidence is preserved per sensor. The low/nominal/high class is a display normalization, not evidence that MODIS and VIIRS confidence values are directly equivalent.",
			"harmonized": "A harmonized cluster is a cross-sensor analytical grouping, not a confirmed fire event or a duplicate assertion.",
			"frp":        "FRP is stored in megawatts as provided by FIRMS; calibration is not applied.",
		},
	})
}
