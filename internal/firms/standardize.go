package firms

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"firms-harmonizer/internal/ptr"
)

type Confidence struct {
	Raw     *string
	Numeric *float64
	Class   *string
	Scheme  *string
}

type NormalizedObservation struct {
	ObservationUID    string
	Sensor            string
	Satellite         string
	SatelliteRaw      *string
	Source            string
	SourceProduct     string
	SourceVersion     *string
	Processing        string
	Latitude          float64
	Longitude         float64
	AcquiredAt        time.Time
	SourceDate        string
	SourceTime        string
	ConfidenceRaw     *string
	ConfidenceNumeric *float64
	ConfidenceClass   *string
	ConfidenceScheme  *string
	FRP               *float64
	FRPUnits          string
	Brightness        *float64
	BrightnessAux     *float64
	Scan              *float64
	Track             *float64
	DayNight          *string
	FireType          *int
	Raw               json.RawMessage
	Warnings          []string
}

type RowError struct {
	Row    int               `json:"row"`
	Reason string            `json:"reason"`
	Raw    map[string]string `json:"raw,omitempty"`
}

type ParseResult struct {
	Observations []NormalizedObservation
	Errors       []RowError
	TotalRows    int
}

var columnAliases = map[string][]string{
	"brightness":     {"brightness", "bright_ti4", "brightness_ti4"},
	"brightness_aux": {"bright_t31", "bright_ti5"},
	"satellite":      {"satellite"},
	"instrument":     {"instrument"},
	"confidence":     {"confidence"},
	"version":        {"version"},
	"frp":            {"frp"},
	"daynight":       {"daynight", "day_night"},
	"fire_type":      {"type"},
	"scan":           {"scan"},
	"track":          {"track"},
}

func ParseCSV(input io.Reader, product Product) (ParseResult, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return ParseResult{}, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return ParseResult{}, fmt.Errorf("read FIRMS CSV header: %w", err)
	}
	for index := range header {
		header[index] = strings.ToLower(strings.TrimSpace(header[index]))
	}
	index := make(map[string]int, len(header))
	for position, name := range header {
		if name != "" {
			index[name] = position
		}
	}
	for _, required := range []string{"latitude", "longitude", "acq_date", "acq_time"} {
		if _, ok := index[required]; !ok {
			preview := string(data)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return ParseResult{}, fmt.Errorf(
				"FIRMS CSV is missing required columns [%s]; response starts with %q",
				required, strings.ReplaceAll(preview, "\n", " "))
		}
	}

	result := ParseResult{}
	for rowNumber := 2; ; rowNumber++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, RowError{Row: rowNumber, Reason: "CSV parse error: " + err.Error()})
			continue
		}
		if allEmpty(record) {
			continue
		}
		result.TotalRows++
		row := make(map[string]string, len(record))
		for position, name := range header {
			if name == "" {
				continue
			}
			if position < len(record) {
				row[name] = strings.TrimSpace(record[position])
			} else {
				row[name] = ""
			}
		}
		observation, err := normalizeRow(row, product)
		if err != nil {
			result.Errors = append(result.Errors, RowError{Row: rowNumber, Reason: err.Error(), Raw: row})
			continue
		}
		result.Observations = append(result.Observations, observation)
	}
	return result, nil
}

func normalizeRow(row map[string]string, product Product) (NormalizedObservation, error) {
	warnings := make([]string, 0, 2)
	get := func(name string) string {
		for _, alias := range columnAliases[name] {
			if value, ok := row[alias]; ok {
				return value
			}
		}
		if value, ok := row[name]; ok {
			return value
		}
		return ""
	}

	latitude, err := optionalFloat(row["latitude"], "latitude")
	if err != nil {
		return NormalizedObservation{}, err
	}
	longitude, err := optionalFloat(row["longitude"], "longitude")
	if err != nil {
		return NormalizedObservation{}, err
	}
	if latitude == nil || longitude == nil {
		return NormalizedObservation{}, fmt.Errorf("missing latitude/longitude")
	}
	if *latitude < -90 || *latitude > 90 {
		return NormalizedObservation{}, fmt.Errorf("latitude out of range: %v", *latitude)
	}
	if *longitude < -180 || *longitude > 180 {
		return NormalizedObservation{}, fmt.Errorf("longitude out of range: %v", *longitude)
	}

	dateText := strings.TrimSpace(row["acq_date"])
	timeText := strings.TrimSpace(row["acq_time"])
	if dateText == "" || timeText == "" {
		return NormalizedObservation{}, fmt.Errorf("missing acq_date/acq_time")
	}
	acquiredDate, err := parseDate(dateText)
	if err != nil {
		return NormalizedObservation{}, err
	}
	sourceTime, hour, minute, err := parseAcqTime(timeText)
	if err != nil {
		return NormalizedObservation{}, err
	}
	acquiredAt := time.Date(
		acquiredDate.Year(), acquiredDate.Month(), acquiredDate.Day(),
		hour, minute, 0, 0, time.UTC)

	instrument := strings.TrimSpace(get("instrument"))
	if instrument != "" && !strings.Contains(strings.ToLower(instrument), strings.ToLower(product.Sensor)) {
		return NormalizedObservation{}, fmt.Errorf(
			"instrument %q is inconsistent with %s source", instrument, product.Sensor)
	}

	satelliteRaw := strings.TrimSpace(get("satellite"))
	satellite, satWarnings := normalizeSatellite(satelliteRaw, product.Sensor, product.DefaultSatellite)
	warnings = append(warnings, satWarnings...)
	confidence := parseConfidence(get("confidence"), product.Sensor)

	frp, err := optionalFloat(get("frp"), "frp")
	if err != nil {
		return NormalizedObservation{}, err
	}
	if frp != nil && *frp < 0 {
		warnings = append(warnings, fmt.Sprintf("negative FRP %v replaced with null", *frp))
		frp = nil
	}
	brightness, err := optionalFloat(get("brightness"), "brightness")
	if err != nil {
		return NormalizedObservation{}, err
	}
	brightnessAux, err := optionalFloat(get("brightness_aux"), "brightness_aux")
	if err != nil {
		return NormalizedObservation{}, err
	}
	scan, err := optionalFloat(get("scan"), "scan")
	if err != nil {
		return NormalizedObservation{}, err
	}
	track, err := optionalFloat(get("track"), "track")
	if err != nil {
		return NormalizedObservation{}, err
	}

	dayNight := strings.ToUpper(strings.TrimSpace(get("daynight")))
	var dayNightPtr *string
	if dayNight == "D" || dayNight == "N" {
		dayNightPtr = ptr.To(dayNight)
	} else if dayNight != "" {
		warnings = append(warnings, fmt.Sprintf("unrecognized daynight %q replaced with null", dayNight))
	}

	var fireType *int
	if rawType := strings.TrimSpace(get("fire_type")); rawType != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(rawType, ".0")))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("unrecognized type %q replaced with null", rawType))
		} else if parsed >= 0 && parsed <= 3 {
			fireType = ptr.To(parsed)
		} else {
			warnings = append(warnings, fmt.Sprintf("type out of range %d replaced with null", parsed))
		}
	}

	rawJSON, err := json.Marshal(row)
	if err != nil {
		return NormalizedObservation{}, fmt.Errorf("encode raw row: %w", err)
	}
	uid := MakeUID(product.Sensor, satellite, acquiredDate, sourceTime, *latitude, *longitude)

	return NormalizedObservation{
		ObservationUID:    uid,
		Sensor:            product.Sensor,
		Satellite:         satellite,
		SatelliteRaw:      optionalString(satelliteRaw),
		Source:            product.Source,
		SourceProduct:     product.Product,
		SourceVersion:     optionalString(get("version")),
		Processing:        product.Processing,
		Latitude:          *latitude,
		Longitude:         *longitude,
		AcquiredAt:        acquiredAt,
		SourceDate:        acquiredDate.Format("2006-01-02"),
		SourceTime:        sourceTime,
		ConfidenceRaw:     confidence.Raw,
		ConfidenceNumeric: confidence.Numeric,
		ConfidenceClass:   confidence.Class,
		ConfidenceScheme:  confidence.Scheme,
		FRP:               frp,
		FRPUnits:          "MW",
		Brightness:        brightness,
		BrightnessAux:     brightnessAux,
		Scan:              scan,
		Track:             track,
		DayNight:          dayNightPtr,
		FireType:          fireType,
		Raw:               rawJSON,
		Warnings:          warnings,
	}, nil
}

func MakeUID(sensor, satellite string, date time.Time, sourceTime string, latitude, longitude float64) string {
	canonical := fmt.Sprintf("%s|%s|%s|%s|%.5f|%.5f",
		sensor, satellite, date.Format("2006-01-02"), sourceTime, latitude, longitude)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func parseConfidence(rawValue, sensor string) Confidence {
	raw := strings.TrimSpace(rawValue)
	if raw == "" {
		return Confidence{}
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "l", "low":
		return Confidence{Raw: ptr.To(raw), Class: ptr.To("low"), Scheme: ptr.To(strings.ToLower(sensor) + "_categorical")}
	case "n", "nominal":
		return Confidence{Raw: ptr.To(raw), Class: ptr.To("nominal"), Scheme: ptr.To(strings.ToLower(sensor) + "_categorical")}
	case "h", "high":
		return Confidence{Raw: ptr.To(raw), Class: ptr.To("high"), Scheme: ptr.To(strings.ToLower(sensor) + "_categorical")}
	}
	if sensor == "MODIS" && strings.HasPrefix(lower, "c") && len(lower) > 1 {
		if rank, err := strconv.Atoi(lower[1:]); err == nil && rank >= 1 && rank <= 9 {
			class := "nominal"
			if rank <= 3 {
				class = "low"
			} else if rank >= 8 {
				class = "high"
			}
			return Confidence{Raw: ptr.To(raw), Class: ptr.To(class), Scheme: ptr.To("modis_c1_c9")}
		}
	}
	numeric, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(numeric) || math.IsInf(numeric, 0) {
		return Confidence{Raw: ptr.To(raw), Scheme: ptr.To("unknown")}
	}
	class := "high"
	if numeric < 30 {
		class = "low"
	} else if numeric < 80 {
		class = "nominal"
	}
	return Confidence{
		Raw:     ptr.To(raw),
		Numeric: ptr.To(numeric),
		Class:   ptr.To(class),
		Scheme:  ptr.To(strings.ToLower(sensor) + "_numeric_0_100"),
	}
}

func normalizeSatellite(raw, sensor, fallback string) (string, []string) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	key := strings.ToUpper(strings.TrimSpace(raw))
	key = strings.NewReplacer("_", " ", "-", " ").Replace(key)
	if sensor == "MODIS" {
		switch key {
		case "TERRA", "EOS AM", "T":
			return "Terra", nil
		case "AQUA", "EOS PM", "A":
			return "Aqua", nil
		}
	} else {
		switch key {
		case "N", "SNPP", "S NPP", "SUOMI NPP", "NPP", "NOAA NPP":
			return "Suomi NPP", nil
		case "1", "J01", "NOAA20", "NOAA 20":
			return "NOAA-20", nil
		case "2", "J02", "NOAA21", "NOAA 21":
			return "NOAA-21", nil
		}
	}
	return fallback, []string{fmt.Sprintf("unrecognized satellite %q; using product default %q", raw, fallback)}
}

func optionalFloat(value, fieldName string) (*float64, error) {
	text := strings.TrimSpace(value)
	switch strings.ToLower(text) {
	case "", "na", "n/a", "nan", "null", "none", "-":
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("%s is not numeric: %q", fieldName, text)
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil, fmt.Errorf("%s is not finite: %q", fieldName, text)
	}
	return &parsed, nil
}

func parseDate(value string) (time.Time, error) {
	text := strings.TrimSpace(value)
	if len(text) >= 10 {
		if parsed, err := time.Parse("2006-01-02", text[:10]); err == nil {
			return parsed, nil
		}
	}
	for _, layout := range []string{"02/01/2006", "01/02/2006", "20060102"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("acq_date is not a valid date: %q", value)
}

func parseAcqTime(value string) (string, int, int, error) {
	text := strings.TrimSpace(value)
	if before, _, found := strings.Cut(text, "."); found {
		text = before
	}
	text = strings.ReplaceAll(text, ":", "")
	if len(text) == 6 {
		text = text[:4]
	}
	for len(text) < 4 {
		text = "0" + text
	}
	if len(text) != 4 {
		return "", 0, 0, fmt.Errorf("acq_time is not HHMM: %q", value)
	}
	hour, errHour := strconv.Atoi(text[:2])
	minute, errMinute := strconv.Atoi(text[2:])
	if errHour != nil || errMinute != nil || hour > 23 || minute > 59 {
		return "", 0, 0, fmt.Errorf("acq_time is out of range: %q", value)
	}
	return text, hour, minute, nil
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func allEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
