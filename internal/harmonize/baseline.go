package harmonize

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
)

const earthRadiusKm = 6371.0088

func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dphi := (lat2 - lat1) * math.Pi / 180
	dlambda := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dphi/2)*math.Sin(dphi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dlambda/2)*math.Sin(dlambda/2)
	if a > 1 {
		a = 1
	}
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}

type gridKey struct {
	lat int
	lon int
}

// BaselineHarmonize is a transparent reference method: every MODIS detection
// opens a cluster, and each VIIRS detection joins the nearest MODIS detection
// within the distance and time thresholds. Unmatched VIIRS detections become
// singleton clusters.
func BaselineHarmonize(observations []Observation, params Params, runUID string) Response {
	modis := make([]Observation, 0)
	viirs := make([]Observation, 0)
	for _, observation := range observations {
		switch observation.Sensor {
		case "MODIS":
			modis = append(modis, observation)
		case "VIIRS":
			viirs = append(viirs, observation)
		}
	}

	if len(modis) == 0 || len(viirs) == 0 {
		clusters := make([]Cluster, 0, len(observations))
		for _, observation := range observations {
			clusters = append(clusters, buildCluster(runUID, []Observation{observation}))
		}
		return Response{
			Clusters: clusters,
			Matches:  []Match{},
			Metrics: map[string]any{
				"method":      "baseline",
				"input_count": len(observations),
				"modis_count": len(modis),
				"viirs_count": len(viirs),
				"match_count": 0,
				"note":        "No cross-sensor matching possible: one sensor is absent.",
			},
		}
	}

	maxAbsLat := 0.0
	for _, observation := range observations {
		if value := math.Abs(observation.Latitude); value > maxAbsLat {
			maxAbsLat = value
		}
	}
	latCell, lonCell := cellSizeDegrees(params.MaxDistanceKm, maxAbsLat)

	grid := make(map[gridKey][]Observation)
	for _, observation := range modis {
		key := gridKey{
			lat: int(math.Floor(observation.Latitude / latCell)),
			lon: int(math.Floor(observation.Longitude / lonCell)),
		}
		grid[key] = append(grid[key], observation)
	}

	assignment := make(map[int64]Observation)
	matches := make([]Match, 0)
	distances := make([]float64, 0)
	timeDeltas := make([]float64, 0)

	for _, target := range viirs {
		keyLat := int(math.Floor(target.Latitude / latCell))
		keyLon := int(math.Floor(target.Longitude / lonCell))
		var best *Observation
		bestDistance := math.MaxFloat64
		bestDelta := math.MaxFloat64
		for deltaLat := -1; deltaLat <= 1; deltaLat++ {
			for deltaLon := -1; deltaLon <= 1; deltaLon++ {
				for index := range grid[gridKey{lat: keyLat + deltaLat, lon: keyLon + deltaLon}] {
					candidate := grid[gridKey{lat: keyLat + deltaLat, lon: keyLon + deltaLon}][index]
					deltaMinutes := math.Abs(target.AcquiredAt.Sub(candidate.AcquiredAt).Minutes())
					if deltaMinutes > params.MaxTimeMinutes {
						continue
					}
					distance := HaversineKm(target.Latitude, target.Longitude, candidate.Latitude, candidate.Longitude)
					if distance > params.MaxDistanceKm {
						continue
					}
					if distance < bestDistance || (distance == bestDistance && deltaMinutes < bestDelta) {
						candidateCopy := candidate
						best = &candidateCopy
						bestDistance = distance
						bestDelta = deltaMinutes
					}
				}
			}
		}
		if best == nil {
			continue
		}
		assignment[target.ObservationID] = *best
		matches = append(matches, Match{
			MODISObservationID: best.ObservationID,
			VIIRSObservationID: target.ObservationID,
			DistanceKm:         round(bestDistance, 4),
			TimeDeltaMinutes:   round(bestDelta, 3),
		})
		distances = append(distances, bestDistance)
		timeDeltas = append(timeDeltas, bestDelta)
	}

	grouped := make(map[int64][]Observation, len(modis))
	for _, observation := range modis {
		grouped[observation.ObservationID] = []Observation{observation}
	}
	for _, observation := range viirs {
		if source, ok := assignment[observation.ObservationID]; ok {
			grouped[source.ObservationID] = append(grouped[source.ObservationID], observation)
		} else {
			grouped[observation.ObservationID] = []Observation{observation}
		}
	}

	clusters := make([]Cluster, 0, len(grouped))
	for _, members := range grouped {
		clusters = append(clusters, buildCluster(runUID, members))
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].StartTime.Equal(clusters[j].StartTime) {
			return clusters[i].ClusterUID < clusters[j].ClusterUID
		}
		return clusters[i].StartTime.Before(clusters[j].StartTime)
	})

	matchedMODIS := make(map[int64]struct{})
	matchedVIIRS := make(map[int64]struct{})
	for _, match := range matches {
		matchedMODIS[match.MODISObservationID] = struct{}{}
		matchedVIIRS[match.VIIRSObservationID] = struct{}{}
	}
	modisOnly, viirsOnly, mixed := 0, 0, 0
	for _, cluster := range clusters {
		switch {
		case cluster.MODISCount > 0 && cluster.VIIRSCount > 0:
			mixed++
		case cluster.MODISCount > 0:
			modisOnly++
		default:
			viirsOnly++
		}
	}

	return Response{
		Clusters: clusters,
		Matches:  matches,
		Metrics: map[string]any{
			"method":                "baseline",
			"params":                params,
			"input_count":           len(observations),
			"modis_count":           len(modis),
			"viirs_count":           len(viirs),
			"cluster_count":         len(clusters),
			"match_count":           len(matches),
			"matched_modis_count":   len(matchedMODIS),
			"matched_viirs_count":   len(matchedVIIRS),
			"unmatched_modis_count": len(modis) - len(matchedMODIS),
			"unmatched_viirs_count": len(viirs) - len(matchedVIIRS),
			"distance_km":           summary(distances),
			"time_delta_minutes":    summary(timeDeltas),
			"clusters_by_composition": map[string]int{
				"modis_only": modisOnly,
				"viirs_only": viirsOnly,
				"mixed":      mixed,
			},
		},
	}
}

func buildCluster(runUID string, members []Observation) Cluster {
	count := len(members)
	var centroidLat, centroidLon float64
	startTime := members[0].AcquiredAt
	endTime := members[0].AcquiredAt
	modisCount := 0
	frpValues := make([]float64, 0, count)
	satelliteSet := make(map[string]struct{})
	confidence := make(map[string]int)
	uids := make([]string, 0, count)

	for _, member := range members {
		centroidLat += member.Latitude
		centroidLon += member.Longitude
		if member.AcquiredAt.Before(startTime) {
			startTime = member.AcquiredAt
		}
		if member.AcquiredAt.After(endTime) {
			endTime = member.AcquiredAt
		}
		if member.Sensor == "MODIS" {
			modisCount++
		}
		if member.FRP != nil {
			frpValues = append(frpValues, *member.FRP)
		}
		satelliteSet[member.Satellite] = struct{}{}
		class := "unknown"
		if member.ConfidenceClass != nil {
			class = *member.ConfidenceClass
		}
		confidence[class]++
		uids = append(uids, member.ObservationUID)
	}
	centroidLat /= float64(count)
	centroidLon /= float64(count)

	satellites := make([]string, 0, len(satelliteSet))
	for satellite := range satelliteSet {
		satellites = append(satellites, satellite)
	}
	sort.Strings(satellites)
	sort.Strings(uids)

	radius := 0.0
	for _, member := range members {
		distance := HaversineKm(centroidLat, centroidLon, member.Latitude, member.Longitude)
		if distance > radius {
			radius = distance
		}
	}

	var totalFRP, meanFRP *float64
	if len(frpValues) > 0 {
		sum := 0.0
		for _, value := range frpValues {
			sum += value
		}
		total := round(sum, 4)
		mean := round(sum/float64(len(frpValues)), 4)
		totalFRP = &total
		meanFRP = &mean
	}

	return Cluster{
		ClusterUID:        clusterUID(runUID, uids),
		Members:           clusterMembers(members),
		CentroidLatitude:  centroidLat,
		CentroidLongitude: centroidLon,
		StartTime:         startTime.UTC(),
		EndTime:           endTime.UTC(),
		ObservationCount:  count,
		MODISCount:        modisCount,
		VIIRSCount:        count - modisCount,
		Satellites:        satellites,
		TotalFRP:          totalFRP,
		MeanFRP:           meanFRP,
		ConfidenceSummary: confidence,
		Uncertainty: map[string]any{
			"spatial_radius_km": round(radius, 4),
			"time_span_minutes": round(endTime.Sub(startTime).Minutes(), 3),
		},
	}
}

func clusterMembers(members []Observation) []Member {
	out := make([]Member, 0, len(members))
	for _, member := range members {
		role := "viirs"
		if member.Sensor == "MODIS" {
			role = "modis"
		}
		out = append(out, Member{ObservationID: member.ObservationID, Role: role})
	}
	return out
}

func clusterUID(runUID string, memberUIDs []string) string {
	hash := sha256.New()
	hash.Write([]byte(runUID))
	for _, uid := range memberUIDs {
		hash.Write([]byte("|"))
		hash.Write([]byte(uid))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cellSizeDegrees(maxDistanceKm, maxAbsLatitude float64) (float64, float64) {
	latDegrees := maxDistanceKm / 110.574
	cosLat := math.Cos(maxAbsLatitude * math.Pi / 180)
	if cosLat < 0.05 {
		cosLat = 0.05
	}
	lonDegrees := maxDistanceKm / (111.320 * cosLat)
	return latDegrees, lonDegrees
}

func summary(values []float64) map[string]any {
	if len(values) == 0 {
		return map[string]any{
			"count": 0, "min": nil, "p50": nil, "mean": nil, "p95": nil, "max": nil,
		}
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	sum := 0.0
	for _, value := range ordered {
		sum += value
	}
	return map[string]any{
		"count": len(ordered),
		"min":   round(ordered[0], 4),
		"p50":   round(percentile(ordered, 0.5), 4),
		"mean":  round(sum/float64(len(ordered)), 4),
		"p95":   round(percentile(ordered, 0.95), 4),
		"max":   round(ordered[len(ordered)-1], 4),
	}
}

func percentile(ordered []float64, fraction float64) float64 {
	if len(ordered) == 1 {
		return ordered[0]
	}
	position := float64(len(ordered)-1) * fraction
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return ordered[lower]
	}
	return ordered[lower] + (ordered[upper]-ordered[lower])*(position-float64(lower))
}

func round(value float64, places int) float64 {
	factor := math.Pow10(places)
	return math.Round(value*factor) / factor
}
