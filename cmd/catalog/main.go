package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pokget/internal/catalog"
	"pokget/internal/catalog/source"
	"pokget/internal/db"
	"pokget/internal/worker"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("Catalog command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	database, err := db.Connect()
	if err != nil {
		return err
	}
	defer database.Close()

	migrations, err := filepath.Abs("migrations")
	if err != nil {
		return fmt.Errorf("catalog: resolve migrations: %w", err)
	}
	if err := db.ApplyMigrations(database, migrations); err != nil {
		return err
	}

	switch args[0] {
	case "sync":
		return runSync(ctx, database, args[1:])
	case "status":
		return runStatus(ctx, database)
	case "verify":
		return runVerify(ctx, database)
	case "images":
		return runImages(ctx, database, args[1:])
	default:
		return usageError()
	}
}

type targetedImageQueue struct {
	repository *catalog.PostgresRepository
	cardID     string
}

func (q targetedImageQueue) LeaseImageJobs(ctx context.Context, owner string, limit int, lease time.Duration) ([]catalog.ImageJob, error) {
	if q.cardID == "" {
		return q.repository.LeaseImageJobs(ctx, owner, limit, lease)
	}
	return q.repository.LeaseImageJobsForCard(ctx, owner, q.cardID, limit, lease)
}

func (q targetedImageQueue) MarkImageReady(ctx context.Context, ready catalog.ReadyImage) error {
	return q.repository.MarkImageReady(ctx, ready)
}

func (q targetedImageQueue) MarkImageFailed(ctx context.Context, failure catalog.ImageFailure) error {
	return q.repository.MarkImageFailed(ctx, failure)
}

func runImages(ctx context.Context, database *sql.DB, args []string) error {
	flags := flag.NewFlagSet("catalog images", flag.ContinueOnError)
	storeDir := flags.String("store", "data/catalog-images", "content-addressed image directory")
	cardID := flags.String("card-id", "", "only process image references for this canonical card ID")
	batchSize := flags.Int("batch-size", 8, "images leased per cycle")
	cycles := flags.Int("cycles", 1, "maximum processing cycles")
	untilIdle := flags.Bool("until-idle", false, "continue until no eligible image jobs remain")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *cycles <= 0 {
		return fmt.Errorf("catalog: image cycles must be positive")
	}
	repository, err := catalog.NewPostgresRepository(database)
	if err != nil {
		return err
	}
	processor, err := catalog.NewImageProcessor(catalog.ImageProcessorConfig{
		StoreDir:     *storeDir,
		AllowedHosts: source.DefaultImageHosts(),
	})
	if err != nil {
		return err
	}
	owner := fmt.Sprintf("catalog-cli:%d", os.Getpid())
	imageWorker, err := worker.NewCatalogImageWorker(
		targetedImageQueue{repository: repository, cardID: strings.TrimSpace(*cardID)},
		processor,
		worker.CatalogImageWorkerConfig{Owner: owner, BatchSize: *batchSize},
	)
	if err != nil {
		return err
	}

	processedTotal := 0
	completedCycles := 0
	for completedCycles < *cycles || *untilIdle {
		processed, cycleErr := imageWorker.RunOnce(ctx)
		processedTotal += processed
		completedCycles++
		if cycleErr != nil {
			return cycleErr
		}
		if processed == 0 {
			break
		}
		if !*untilIdle && completedCycles >= *cycles {
			break
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]interface{}{
		"card_id":   strings.TrimSpace(*cardID),
		"cycles":    completedCycles,
		"processed": processedTotal,
		"store":     *storeDir,
	})
}

func runSync(ctx context.Context, database *sql.DB, args []string) error {
	flags := flag.NewFlagSet("catalog sync", flag.ContinueOnError)
	gameName := flags.String("game", "all", "pokemon, magic, one_piece, lorcana, weiss_schwarz, yugioh, or all")
	modeName := flags.String("mode", string(catalog.SyncModeIncremental), "full or incremental")
	language := flags.String("lang", "en", "catalog language where supported")
	batchSize := flags.Int("batch-size", 500, "database upsert batch size")
	timeout := flags.Duration("timeout", 6*time.Hour, "per-source sync timeout")
	requestDelay := flags.Duration("request-delay", 100*time.Millisecond, "delay between paginated upstream requests")
	maxWeissPages := flags.Int("weiss-max-pages", 0, "limit Weiss pages for diagnostics; 0 means complete catalog")
	if err := flags.Parse(args); err != nil {
		return err
	}
	mode := catalog.SyncMode(*modeName)
	if !mode.Valid() {
		return fmt.Errorf("catalog: invalid sync mode %q", *modeName)
	}
	repository, err := catalog.NewPostgresRepository(database)
	if err != nil {
		return err
	}
	providers, err := providersFor(*gameName, *language, *requestDelay, *maxWeissPages)
	if err != nil {
		return err
	}
	syncer := &catalog.Syncer{Repository: repository, BatchSize: *batchSize}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	for _, provider := range providers {
		state, err := repository.SourceState(ctx, provider.ID())
		if err != nil {
			return err
		}
		sourceCtx, cancel := context.WithTimeout(ctx, *timeout)
		completion, syncErr := syncer.Sync(sourceCtx, provider, mode, state.FetchRequest(mode))
		cancel()
		if syncErr != nil {
			return fmt.Errorf("catalog: sync %s: %w", provider.ID(), syncErr)
		}
		if err := encoder.Encode(map[string]interface{}{
			"source":     provider.ID(),
			"game":       provider.Game(),
			"completion": completion,
		}); err != nil {
			return err
		}
	}
	return nil
}

func providersFor(gameName, language string, delay time.Duration, maxWeissPages int) ([]catalog.Provider, error) {
	httpOptions := source.HTTPOptions{
		Client:       &http.Client{Timeout: 5 * time.Minute},
		UserAgent:    "pokget-catalog/1.0",
		MaxBodyBytes: 512 << 20,
		RequestDelay: delay,
	}
	registered := source.DefaultProviders(httpOptions, language, maxWeissPages)
	if gameName == "all" {
		return registered, nil
	}
	game := catalog.Game(strings.ToLower(gameName))
	for _, provider := range registered {
		if provider.Game() == game {
			return []catalog.Provider{provider}, nil
		}
	}
	return nil, fmt.Errorf("catalog: unsupported game %q", gameName)
}

func runStatus(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `
		SELECT source.id, source.game, source.name, source.enabled,
		       state.upstream_version, state.last_attempt_at, state.last_success_at,
		       state.last_full_sync_at, state.last_record_count,
		       state.consecutive_failures, state.last_error
		FROM catalog_sources AS source
		JOIN catalog_source_state AS state ON state.source_id = source.id
		ORDER BY source.game, source.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	return encodeRows(rows, []string{"source", "game", "name", "enabled", "upstream_version", "last_attempt_at", "last_success_at", "last_full_sync_at", "record_count", "consecutive_failures", "last_error"})
}

func runVerify(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `
		SELECT source.id, source.game,
		       COUNT(card.id) FILTER (WHERE card.catalog_active),
		       COUNT(image.id) FILTER (WHERE card.catalog_active),
		       COUNT(image.id) FILTER (WHERE card.catalog_active AND image.status = 'ready'),
		       COUNT(fingerprint.image_id) FILTER (WHERE card.catalog_active)
		FROM catalog_sources AS source
		LEFT JOIN cards AS card ON card.source_id = source.id
		LEFT JOIN card_images AS image ON image.card_id = card.id
		LEFT JOIN card_fingerprints AS fingerprint ON fingerprint.image_id = image.id
		GROUP BY source.id, source.game
		ORDER BY source.game, source.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	return encodeRows(rows, []string{"source", "game", "active_cards", "image_references", "ready_images", "fingerprints"})
}

func encodeRows(rows *sql.Rows, columns []string) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	for rows.Next() {
		values := make([]interface{}, len(columns))
		destinations := make([]interface{}, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return err
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			if bytes, ok := values[i].([]byte); ok {
				row[column] = string(bytes)
			} else {
				row[column] = values[i]
			}
		}
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func usageError() error {
	return errors.New("usage: catalog <sync|status|verify|images> [flags]")
}
