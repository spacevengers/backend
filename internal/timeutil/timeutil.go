package timeutil

import (
	"fmt"
	"time"
)

// ParseBound parses a date or RFC3339 datetime. Date-only end bounds are
// expanded to the end of that UTC day.
func ParseBound(value string, endOfDay bool) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDay {
			return parsed.Add(24*time.Hour - time.Microsecond).UTC(), nil
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid date/datetime %q", value)
}

func Milli(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

func FromMilli(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

func FromMilliPtr(ms *int64) *time.Time {
	if ms == nil {
		return nil
	}
	value := FromMilli(*ms)
	return &value
}

func FormatZ(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func FormatZPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	value := FormatZ(*t)
	return &value
}
