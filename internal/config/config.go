package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is loaded from environment variables, with an optional .env file.
type Config struct {
	DatabasePath string
	AutoMigrate  bool

	FirmsMapKey        string
	FirmsBaseURL       string
	FirmsTimeout       time.Duration
	FirmsMaxRetries    int
	FirmsRequestDelay  time.Duration
	FirmsMaxDays       int
	DefaultBBox        string
	AdminToken         string
	CORSOrigins        []string
	ExportDir          string
	MaxExportRows      int
	APIAddr            string
	SchemaVersion      string
	HarmonizerMethod   string
	HarmonizerEndpoint string
	HarmonizerTimeout  time.Duration
	HarmonizeMaxKm     float64
	HarmonizeMaxMin    float64
	SynchronousJobs    bool
}

func Load() Config {
	loadDotEnv(".env")
	cwd, _ := os.Getwd()

	cfg := Config{
		DatabasePath:       env("DATABASE_PATH", filepath.Join(cwd, "data", "firms.db")),
		AutoMigrate:        envBool("AUTO_MIGRATE", true),
		FirmsMapKey:        env("FIRMS_MAP_KEY", ""),
		FirmsBaseURL:       strings.TrimRight(env("FIRMS_BASE_URL", "https://firms.modaps.eosdis.nasa.gov"), "/"),
		FirmsTimeout:       envDuration("FIRMS_TIMEOUT_SECONDS", 60*time.Second),
		FirmsMaxRetries:    envInt("FIRMS_MAX_RETRIES", 4),
		FirmsRequestDelay:  envDuration("FIRMS_REQUEST_DELAY_SECONDS", 500*time.Millisecond),
		FirmsMaxDays:       envInt("FIRMS_MAX_DAYS_PER_REQUEST", 5),
		DefaultBBox:        env("DEFAULT_BBOX", "33.5,-5.0,42.0,5.5"),
		AdminToken:         env("ADMIN_TOKEN", ""),
		CORSOrigins:        splitCSV(env("CORS_ORIGINS", "http://localhost:3000,http://localhost:3100,http://localhost:5173")),
		ExportDir:          absPath(env("EXPORT_DIR", filepath.Join(cwd, "exports"))),
		MaxExportRows:      envInt("MAX_EXPORT_ROWS", 500000),
		APIAddr:            env("API_ADDR", ":9999"),
		SchemaVersion:      env("SCHEMA_VERSION", "0.1.0"),
		HarmonizerMethod:   env("HARMONIZER_METHOD", "baseline"),
		HarmonizerEndpoint: env("HARMONIZER_ENDPOINT", ""),
		HarmonizerTimeout:  envDuration("HARMONIZER_TIMEOUT_SECONDS", 300*time.Second),
		HarmonizeMaxKm:     envFloat("HARMONIZATION_MAX_DISTANCE_KM", 2.0),
		HarmonizeMaxMin:    envFloat("HARMONIZATION_MAX_TIME_MINUTES", 180.0),
		SynchronousJobs:    envBool("SYNC_JOBS", false),
	}
	return cfg
}

func (c Config) DefaultBBoxParts() (string, bool) {
	return c.DefaultBBox, strings.TrimSpace(c.DefaultBBox) != ""
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(env(key, ""), 64)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(env(key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	seconds, err := strconv.ParseFloat(env(key, ""), 64)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return resolved
}
