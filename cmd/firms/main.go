package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"firms-harmonizer/internal/api"
	"firms-harmonizer/internal/config"
	"firms-harmonizer/internal/db"
	"firms-harmonizer/internal/firms"
	"firms-harmonizer/internal/harmonize"
	"firms-harmonizer/internal/ingest"
	"firms-harmonizer/internal/query"
	"firms-harmonizer/internal/store"
	"firms-harmonizer/internal/timeutil"
)

func main() {
	cfg := config.Load()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(cfg, os.Args[2:])
	case "init-db":
		err = runInitDB(cfg, os.Args[2:])
	case "ingest":
		err = runIngest(cfg, os.Args[2:])
	case "ingest-file":
		err = runIngestFile(cfg, os.Args[2:])
	case "harmonize":
		err = runHarmonize(cfg, os.Args[2:])
	case "import-predictions":
		err = runImportPredictions(cfg, os.Args[2:])
	case "stats":
		err = runStats(cfg, os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `FIRMS harmonizer backend

Usage:
  firms serve        Start the HTTP API
  firms init-db      Apply database migrations
  firms ingest       Ingest FIRMS data for a date range
  firms ingest-file  Ingest a local FIRMS CSV
  firms harmonize    Run spatial-temporal matching
  firms import-predictions  Import ML predictions for a harmonization run
  firms stats        Count stored observations`)
}

func openStore(cfg config.Config, migrate bool) (*store.Store, error) {
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if migrate {
		if err := db.Migrate(context.Background(), database); err != nil {
			database.Close()
			return nil, err
		}
	}
	return store.New(database), nil
}

func runInitDB(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("init-db", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	repository, err := openStore(cfg, true)
	if err != nil {
		return err
	}
	defer repository.Close()
	return printJSON(map[string]any{"status": "ok", "database": cfg.DatabasePath})
}

func runServe(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", cfg.APIAddr, "listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	server := &http.Server{
		Addr:              *addr,
		Handler:           api.New(cfg, repository).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("listening on %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func runIngest(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("ingest", flag.ContinueOnError)
	source := flags.String("source", "", "FIRMS source ID")
	start := flags.String("start", "", "YYYY-MM-DD")
	end := flags.String("end", "", "YYYY-MM-DD")
	bbox := flags.String("bbox", cfg.DefaultBBox, "west,south,east,north")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *source == "" || *start == "" || *end == "" {
		return fmt.Errorf("--source, --start, and --end are required")
	}
	product, err := firms.GetProduct(*source)
	if err != nil {
		return err
	}
	startDate, err := timeutil.ParseBound(*start, false)
	if err != nil {
		return err
	}
	endDate, err := timeutil.ParseBound(*end, true)
	if err != nil {
		return err
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	ctx := context.Background()
	run, err := ingest.CreateRun(ctx, repository, product, *bbox, startDate, endDate, "cli")
	if err != nil {
		return err
	}
	client := firms.NewClient(cfg.FirmsMapKey, cfg.FirmsBaseURL, cfg.FirmsTimeout,
		cfg.FirmsMaxRetries, cfg.FirmsMaxDays, cfg.FirmsRequestDelay)
	run, err = ingest.IngestProduct(ctx, repository, run, product, *bbox, startDate, endDate, client)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"run_id": run.ID, "status": run.Status, "source": run.Source,
		"area": run.Area, "start_date": run.StartDate, "end_date": run.EndDate,
		"fetched": run.FetchedCount, "inserted": run.InsertedCount,
		"duplicates": run.DuplicateCount, "rejected": run.RejectedCount,
	})
}

func runIngestFile(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("ingest-file", flag.ContinueOnError)
	source := flags.String("source", "", "FIRMS source ID")
	file := flags.String("file", "", "path to FIRMS CSV")
	start := flags.String("start", "", "optional YYYY-MM-DD")
	end := flags.String("end", "", "optional YYYY-MM-DD")
	bbox := flags.String("bbox", cfg.DefaultBBox, "west,south,east,north")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *source == "" || *file == "" {
		return fmt.Errorf("--source and --file are required")
	}
	product, err := firms.GetProduct(*source)
	if err != nil {
		return err
	}
	text, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	parsed, err := firms.ParseCSV(strings.NewReader(string(text)), product)
	if err != nil {
		return err
	}
	if len(parsed.Observations) == 0 {
		return fmt.Errorf("no valid observations in %s", *file)
	}
	startDate := parsed.Observations[0].AcquiredAt
	endDate := parsed.Observations[0].AcquiredAt
	for _, observation := range parsed.Observations {
		if observation.AcquiredAt.Before(startDate) {
			startDate = observation.AcquiredAt
		}
		if observation.AcquiredAt.After(endDate) {
			endDate = observation.AcquiredAt
		}
	}
	if *start != "" {
		startDate, err = timeutil.ParseBound(*start, false)
		if err != nil {
			return err
		}
	}
	if *end != "" {
		endDate, err = timeutil.ParseBound(*end, true)
		if err != nil {
			return err
		}
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	ctx := context.Background()
	run, err := ingest.CreateRun(ctx, repository, product, *bbox, startDate, endDate, "cli-file")
	if err != nil {
		return err
	}
	stats, err := ingest.IngestText(ctx, repository, run, product, string(text))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	run.Status = "completed"
	run.StartedAt = &now
	run.CompletedAt = &now
	if err := repository.UpdateIngestionRun(ctx, run); err != nil {
		return err
	}
	return printJSON(map[string]any{
		"run_id": run.ID, "status": run.Status, "file": *file,
		"inserted": stats.Inserted, "duplicates": stats.Duplicates,
		"rejected": stats.Rejected,
	})
}

func runHarmonize(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("harmonize", flag.ContinueOnError)
	method := flags.String("method", cfg.HarmonizerMethod, "baseline or remote")
	start := flags.String("start", "", "optional start")
	end := flags.String("end", "", "optional end")
	bbox := flags.String("bbox", cfg.DefaultBBox, "west,south,east,north")
	sensor := flags.String("sensor", "", "optional MODIS or VIIRS")
	maxDistance := flags.Float64("max-distance", cfg.HarmonizeMaxKm, "km")
	maxTime := flags.Float64("max-time", cfg.HarmonizeMaxMin, "minutes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *method != "baseline" && *method != "remote" {
		return fmt.Errorf("--method must be baseline or remote")
	}
	startTime, endTime, err := parseOptionalRange(*start, *end)
	if err != nil {
		return err
	}
	parsedBBox, err := query.ParseBBox(*bbox)
	if err != nil {
		return err
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	ctx := context.Background()
	run, err := harmonize.RunSync(ctx, repository, *method, query.ObservationFilter{
		Sensor: *sensor, Start: startTime, End: endTime, BBox: parsedBBox,
	}, harmonize.Params{
		MaxDistanceKm: *maxDistance, MaxTimeMinutes: *maxTime,
	}, cfg.HarmonizerEndpoint, cfg.HarmonizerTimeout)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"run_id": run.ID, "run_uid": run.RunUID, "status": run.Status,
		"method": run.Method, "input_count": run.InputCount,
		"cluster_count": run.ClusterCount, "match_count": run.MatchCount,
		"metrics": json.RawMessage(run.Metrics),
	})
}

func runImportPredictions(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("import-predictions", flag.ContinueOnError)
	runID := flags.Int64("run-id", 0, "completed harmonization run id")
	file := flags.String("file", "", "JSON file with {model, predictions[]} or a predictions[] array")
	model := flags.String("model", "external", "model/version label")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runID <= 0 || *file == "" {
		return fmt.Errorf("--run-id and --file are required")
	}
	payload, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var envelope struct {
		Model       string             `json:"model"`
		Predictions []store.Prediction `json:"predictions"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || len(envelope.Predictions) == 0 {
		var predictions []store.Prediction
		if err := json.Unmarshal(payload, &predictions); err != nil {
			return fmt.Errorf("prediction file must be {model, predictions[]} or an array: %w", err)
		}
		envelope.Predictions = predictions
	}
	if envelope.Model != "" {
		*model = envelope.Model
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	ctx := context.Background()
	run, err := repository.GetHarmonizationRun(ctx, *runID)
	if err != nil {
		return err
	}
	if run.Status != "completed" {
		return fmt.Errorf("run %d is %q, not completed", run.ID, run.Status)
	}
	updated, err := repository.UpdatePredictions(ctx, run.ID, *model, envelope.Predictions)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"run_id": run.ID, "model": *model, "updated": updated,
	})
}

func runStats(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("stats", flag.ContinueOnError)
	start := flags.String("start", "", "optional start")
	end := flags.String("end", "", "optional end")
	bbox := flags.String("bbox", "", "optional west,south,east,north")
	if err := flags.Parse(args); err != nil {
		return err
	}
	startTime, endTime, err := parseOptionalRange(*start, *end)
	if err != nil {
		return err
	}
	var parsedBBox *query.BBox
	if *bbox != "" {
		parsedBBox, err = query.ParseBBox(*bbox)
		if err != nil {
			return err
		}
	}
	repository, err := openStore(cfg, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer repository.Close()

	filter := query.ObservationFilter{Start: startTime, End: endTime, BBox: parsedBBox}
	total, err := repository.CountObservations(context.Background(), filter)
	if err != nil {
		return err
	}
	counts, err := repository.CountsBySensor(context.Background(), filter)
	if err != nil {
		return err
	}
	bySensor := make(map[string]int, len(counts))
	for _, count := range counts {
		bySensor[count.Sensor] = count.Count
	}
	return printJSON(map[string]any{"total": total, "by_sensor": bySensor})
}

func parseOptionalRange(start, end string) (*time.Time, *time.Time, error) {
	var startTime, endTime *time.Time
	if start != "" {
		parsed, err := timeutil.ParseBound(start, false)
		if err != nil {
			return nil, nil, err
		}
		startTime = &parsed
	}
	if end != "" {
		parsed, err := timeutil.ParseBound(end, true)
		if err != nil {
			return nil, nil, err
		}
		endTime = &parsed
	}
	return startTime, endTime, nil
}

func printJSON(payload any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
