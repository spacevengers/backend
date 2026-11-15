package firms

import (
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseMODIS(t *testing.T) {
	product, _ := GetProduct("MODIS_SP")
	result, err := ParseCSV(strings.NewReader(readFixture(t, "modis_sample.csv")), product)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRows != 3 || len(result.Observations) != 3 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	first := result.Observations[0]
	if first.Sensor != "MODIS" || first.Satellite != "Terra" {
		t.Fatalf("unexpected sensor/satellite: %+v", first)
	}
	if first.SourceProduct != "MCD14ML" || first.Processing != "SP" {
		t.Fatalf("unexpected product: %+v", first)
	}
	if first.AcquiredAt.Format(time.RFC3339) != "2024-01-01T10:30:00Z" {
		t.Fatalf("unexpected time: %s", first.AcquiredAt)
	}
	if first.ConfidenceRaw == nil || *first.ConfidenceRaw != "85" {
		t.Fatalf("raw confidence not preserved: %+v", first.ConfidenceRaw)
	}
	if first.ConfidenceNumeric == nil || *first.ConfidenceNumeric != 85 {
		t.Fatalf("numeric confidence not parsed")
	}
	if first.ConfidenceClass == nil || *first.ConfidenceClass != "high" {
		t.Fatalf("unexpected confidence class")
	}
	if first.FRP == nil || *first.FRP != 12.5 {
		t.Fatalf("unexpected FRP")
	}

	second := result.Observations[1]
	if second.ConfidenceRaw == nil || *second.ConfidenceRaw != "c8" {
		t.Fatalf("class confidence not preserved")
	}
	if second.ConfidenceNumeric != nil {
		t.Fatalf("class confidence must not invent a numeric value")
	}
	if second.ConfidenceClass == nil || *second.ConfidenceClass != "high" {
		t.Fatalf("unexpected class mapping")
	}
}

func TestParseVIIRS(t *testing.T) {
	product, _ := GetProduct("VIIRS_SNPP_SP")
	result, err := ParseCSV(strings.NewReader(readFixture(t, "viirs_sample.csv")), product)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 3 {
		t.Fatalf("expected 3 observations, got %d", len(result.Observations))
	}
	high := result.Observations[0]
	if high.Satellite != "Suomi NPP" {
		t.Fatalf("unexpected satellite: %s", high.Satellite)
	}
	if high.ConfidenceNumeric != nil || high.ConfidenceClass == nil || *high.ConfidenceClass != "high" {
		t.Fatalf("unexpected VIIRS confidence: %+v", high)
	}
}

func TestUIDStableAcrossProcessing(t *testing.T) {
	modis, _ := GetProduct("MODIS_SP")
	nrt, _ := GetProduct("MODIS_NRT")
	spResult, _ := ParseCSV(strings.NewReader(readFixture(t, "modis_sample.csv")), modis)
	nrtResult, _ := ParseCSV(strings.NewReader(readFixture(t, "modis_sample.csv")), nrt)
	if spResult.Observations[0].ObservationUID != nrtResult.Observations[0].ObservationUID {
		t.Fatalf("UID should not depend on source processing")
	}
}

func TestRejectsInvalidRows(t *testing.T) {
	csvText := "latitude,longitude,acq_date,acq_time,satellite,instrument,confidence,frp\n" +
		"not-a-number,36.8,2024-01-01,1030,Terra,MODIS,80,1.0\n" +
		"-1.29,36.82,2024-01-01,2460,Terra,MODIS,80,1.0\n" +
		"-1.29,36.82,not-a-date,1030,Terra,MODIS,80,1.0\n" +
		"-1.29,36.82,2024-01-01,1030,Terra,VIIRS,80,1.0\n" +
		"-1.29,36.82,2024-01-01,1030,Terra,MODIS,80,1.0\n"
	product, _ := GetProduct("MODIS_SP")
	result, err := ParseCSV(strings.NewReader(csvText), product)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRows != 5 || len(result.Observations) != 1 || len(result.Errors) != 4 {
		t.Fatalf("unexpected validation result: %+v", result)
	}
}

func TestMissingColumns(t *testing.T) {
	product, _ := GetProduct("MODIS_SP")
	if _, err := ParseCSV(strings.NewReader("foo,bar\n1,2\n"), product); err == nil {
		t.Fatal("expected an error for missing columns")
	}
}
