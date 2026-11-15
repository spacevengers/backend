package harmonize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"firms-harmonizer/internal/models"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
)

const MaxInputObservations = 250_000

func CreateRun(
	ctx context.Context,
	repository *store.Store,
	method string,
	filters query.ObservationFilter,
	params Params,
) (*models.HarmonizationRun, error) {
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	run := &models.HarmonizationRun{
		RunUID:    uuid.NewString(),
		Method:    method,
		Status:    "pending",
		Params:    models.JSONText(paramsJSON),
		Filters:   models.JSONText(filtersJSON),
		Metrics:   models.JSONText("{}"),
		CreatedAt: time.Now().UTC(),
	}
	if err := repository.CreateHarmonizationRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func Execute(
	ctx context.Context,
	repository *store.Store,
	runID int64,
	endpoint string,
	timeout time.Duration,
) (*models.HarmonizationRun, error) {
	run, err := repository.GetHarmonizationRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "pending" && run.Status != "failed" {
		return run, fmt.Errorf("run status is %q, not executable", run.Status)
	}

	now := time.Now().UTC()
	run.Status = "running"
	run.StartedAt = &now
	run.Error = nil
	if err := repository.UpdateHarmonizationRun(ctx, run); err != nil {
		return run, err
	}

	fail := func(cause error) (*models.HarmonizationRun, error) {
		completed := time.Now().UTC()
		run.Status = "failed"
		run.Error = ptrString(cause.Error())
		run.CompletedAt = &completed
		_ = repository.UpdateHarmonizationRun(ctx, run)
		return run, cause
	}

	var filter query.ObservationFilter
	if err := json.Unmarshal(run.Filters, &filter); err != nil {
		return fail(fmt.Errorf("decode run filters: %w", err))
	}
	total, err := repository.CountObservations(ctx, filter)
	if err != nil {
		return fail(err)
	}
	if total == 0 {
		return fail(fmt.Errorf("no observations match the requested filters"))
	}
	if total > MaxInputObservations {
		return fail(fmt.Errorf(
			"%d observations match; limit is %d, narrow the date range or bounding box",
			total, MaxInputObservations))
	}

	rows, err := repository.ListObservations(
		ctx, filter, "acquired_at", "asc", MaxInputObservations, 0)
	if err != nil {
		return fail(err)
	}
	observations := make([]Observation, 0, len(rows))
	for _, row := range rows {
		observations = append(observations, toContract(row))
	}
	run.InputCount = len(observations)

	var params Params
	if err := json.Unmarshal(run.Params, &params); err != nil {
		return fail(fmt.Errorf("decode run parameters: %w", err))
	}

	var response Response
	switch run.Method {
	case "baseline":
		response = BaselineHarmonize(observations, params, run.RunUID)
	case "remote":
		if endpoint == "" {
			return fail(fmt.Errorf("HARMONIZER_ENDPOINT must be set for the remote method"))
		}
		response, err = callRemote(ctx, endpoint, timeout, Request{
			RunUID:       run.RunUID,
			Method:       run.Method,
			Params:       params,
			Observations: observations,
		})
		if err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("unknown harmonization method %q", run.Method))
	}

	if err := validateResponse(response, observations); err != nil {
		return fail(err)
	}
	if response.Metrics == nil {
		response.Metrics = map[string]any{}
	}
	clusters := make([]models.HarmonizedObservation, 0, len(response.Clusters))
	for _, cluster := range response.Clusters {
		clusters = append(clusters, clusterModel(cluster))
	}
	matches := make([]models.ObservationMatch, 0, len(response.Matches))
	for _, match := range response.Matches {
		matches = append(matches, matchModel(run.ID, run.Method, match))
	}
	if err := repository.ReplaceHarmonizationOutputs(ctx, run.ID, clusters, matches); err != nil {
		return fail(err)
	}

	metricsJSON, err := json.Marshal(response.Metrics)
	if err != nil {
		return fail(err)
	}
	completed := time.Now().UTC()
	run.Status = "completed"
	run.CompletedAt = &completed
	run.ClusterCount = len(response.Clusters)
	run.MatchCount = len(response.Matches)
	run.Metrics = models.JSONText(metricsJSON)
	if err := repository.UpdateHarmonizationRun(ctx, run); err != nil {
		return run, err
	}
	return run, nil
}

func RunSync(
	ctx context.Context,
	repository *store.Store,
	method string,
	filters query.ObservationFilter,
	params Params,
	endpoint string,
	timeout time.Duration,
) (*models.HarmonizationRun, error) {
	run, err := CreateRun(ctx, repository, method, filters, params)
	if err != nil {
		return nil, err
	}
	return Execute(ctx, repository, run.ID, endpoint, timeout)
}

func callRemote(ctx context.Context, endpoint string, timeout time.Duration, request Request) (Response, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(httpRequest)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 128<<20))
	if err != nil {
		return Response{}, err
	}
	if response.StatusCode >= 400 {
		preview := string(payload)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		return Response{}, fmt.Errorf("harmonizer service returned HTTP %d: %s", response.StatusCode, preview)
	}
	var decoded Response
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Response{}, fmt.Errorf("decode harmonizer response: %w", err)
	}
	return decoded, nil
}

func validateResponse(response Response, observations []Observation) error {
	sensors := make(map[int64]string, len(observations))
	for _, observation := range observations {
		sensors[observation.ObservationID] = observation.Sensor
	}
	seen := make(map[int64]struct{}, len(observations))
	clusterUIDs := make(map[string]struct{}, len(response.Clusters))

	for _, cluster := range response.Clusters {
		if _, exists := clusterUIDs[cluster.ClusterUID]; exists {
			return fmt.Errorf("duplicate cluster_uid in harmonizer response: %s", cluster.ClusterUID)
		}
		clusterUIDs[cluster.ClusterUID] = struct{}{}
		if len(cluster.Members) == 0 {
			return fmt.Errorf("cluster %s has no members", cluster.ClusterUID)
		}
		computedMODIS := 0
		computedVIIRS := 0
		for _, member := range cluster.Members {
			sensor, ok := sensors[member.ObservationID]
			if !ok {
				return fmt.Errorf("cluster %s references unknown observation %d", cluster.ClusterUID, member.ObservationID)
			}
			if _, duplicate := seen[member.ObservationID]; duplicate {
				return fmt.Errorf("observation %d appears in more than one cluster", member.ObservationID)
			}
			seen[member.ObservationID] = struct{}{}
			expectedRole := "viirs"
			if sensor == "MODIS" {
				expectedRole = "modis"
				computedMODIS++
			} else {
				computedVIIRS++
			}
			if member.Role != expectedRole {
				return fmt.Errorf("cluster %s role %q does not match %s observation %d",
					cluster.ClusterUID, member.Role, sensor, member.ObservationID)
			}
		}
		if cluster.ObservationCount != len(cluster.Members) ||
			cluster.MODISCount != computedMODIS || cluster.VIIRSCount != computedVIIRS {
			return fmt.Errorf("cluster %s has inconsistent member counts", cluster.ClusterUID)
		}
		if cluster.PModisDetect != nil && (*cluster.PModisDetect < 0 || *cluster.PModisDetect > 1) {
			return fmt.Errorf("cluster %s p_modis_detect must be in [0, 1]", cluster.ClusterUID)
		}
		if cluster.FRPCalibrated != nil && *cluster.FRPCalibrated < 0 {
			return fmt.Errorf("cluster %s frp_calibrated must be non-negative", cluster.ClusterUID)
		}
	}
	if len(seen) != len(sensors) {
		return fmt.Errorf("harmonizer response covers %d of %d observations", len(seen), len(sensors))
	}

	for _, match := range response.Matches {
		if sensors[match.MODISObservationID] != "MODIS" {
			return fmt.Errorf("match MODIS id %d is not a MODIS observation", match.MODISObservationID)
		}
		if sensors[match.VIIRSObservationID] != "VIIRS" {
			return fmt.Errorf("match VIIRS id %d is not a VIIRS observation", match.VIIRSObservationID)
		}
	}
	return nil
}

func toContract(observation models.Observation) Observation {
	return Observation{
		ObservationID:     observation.ID,
		ObservationUID:    observation.ObservationUID,
		Sensor:            observation.Sensor,
		Satellite:         observation.Satellite,
		Latitude:          observation.Latitude,
		Longitude:         observation.Longitude,
		AcquiredAt:        observation.AcquiredAt,
		ConfidenceRaw:     observation.ConfidenceRaw,
		ConfidenceNumeric: observation.ConfidenceNumeric,
		ConfidenceClass:   observation.ConfidenceClass,
		ConfidenceScheme:  observation.ConfidenceScheme,
		FRP:               observation.FRP,
		FRPUnits:          observation.FRPUnits,
	}
}

func clusterModel(cluster Cluster) models.HarmonizedObservation {
	satellites, _ := json.Marshal(cluster.Satellites)
	confidence, _ := json.Marshal(cluster.ConfidenceSummary)
	uncertainty, _ := json.Marshal(cluster.Uncertainty)
	members := make([]models.HarmonizedMember, 0, len(cluster.Members))
	for _, member := range cluster.Members {
		members = append(members, models.HarmonizedMember{
			ObservationID: member.ObservationID,
			Role:          member.Role,
		})
	}
	return models.HarmonizedObservation{
		ClusterUID:        cluster.ClusterUID,
		CentroidLatitude:  cluster.CentroidLatitude,
		CentroidLongitude: cluster.CentroidLongitude,
		StartTime:         cluster.StartTime,
		EndTime:           cluster.EndTime,
		ObservationCount:  cluster.ObservationCount,
		MODISCount:        cluster.MODISCount,
		VIIRSCount:        cluster.VIIRSCount,
		Satellites:        models.JSONText(satellites),
		TotalFRP:          cluster.TotalFRP,
		MeanFRP:           cluster.MeanFRP,
		ConfidenceSummary: models.JSONText(confidence),
		Uncertainty:       models.JSONText(uncertainty),
		PModisDetect:      cluster.PModisDetect,
		FRPCalibrated:     cluster.FRPCalibrated,
		PredictionModel:   optionalString(cluster.PredictionModel),
		Members:           members,
	}
}

func matchModel(runID int64, method string, match Match) models.ObservationMatch {
	return models.ObservationMatch{
		RunID:              runID,
		MODISObservationID: match.MODISObservationID,
		VIIRSObservationID: match.VIIRSObservationID,
		DistanceKm:         match.DistanceKm,
		TimeDeltaMinutes:   match.TimeDeltaMinutes,
		Method:             method,
	}
}

func ptrString(value string) *string {
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
