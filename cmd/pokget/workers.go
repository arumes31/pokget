package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"pokget/internal/config"
	"pokget/internal/models"
	"pokget/internal/service"
	"pokget/internal/worker"
)

func newDataSyncWorker(
	database *sql.DB,
	cfg *config.Config,
	fingerprint *service.FingerprintService,
) (*worker.DataSyncWorker, error) {
	return newDataSyncWorkerWithClients(database, cfg, fingerprint, defaultPriceClients())
}

// defaultPriceClients builds the per-game price source chains. The same chains
// back the sync worker and the on-demand lookups, so the TCGplayer browser is
// shared instead of started twice.
func defaultPriceClients() map[string][]service.PriceClient {
	priceClient := &service.DefaultPriceClient{Scraper: service.NewScraperPriceClient()}
	// Cardmarket blocks server-side scraping with Cloudflare (HTTP 403), so the
	// games with a public price API are served by that API first. TCGplayer still
	// provides the USD side for every other game.
	return map[string][]service.PriceClient{
		"pokemon": {&service.TCGdexPriceClient{Language: "en"}},
		"magic":   {&service.ScryfallPriceClient{}},
		"yugioh":  {&service.YGOPRODeckPriceClient{}},
		"*":       {priceClient},
	}
}

func newDataSyncWorkerWithClients(
	database *sql.DB,
	cfg *config.Config,
	fingerprint *service.FingerprintService,
	priceClients map[string][]service.PriceClient,
) (*worker.DataSyncWorker, error) {
	var metadataClient service.MetadataClient
	var metadataService *service.MetadataService
	if !cfg.Catalog.Enabled && cfg.Catalog.LegacyMetadataSync {
		metadataClient = service.NewTCGDexClient()
		metadataService = service.NewMetadataService(fingerprint)
	}

	failureSink, err := worker.NewFileFailureSink(cfg.Worker.FailurePath)
	if err != nil {
		return nil, err
	}
	var fallback service.PriceClient
	if chain := priceClients["*"]; len(chain) > 0 {
		fallback = chain[0]
	}
	return worker.NewConfiguredDataSyncWorker(
		database,
		fallback,
		metadataClient,
		metadataService,
		worker.DataSyncConfig{
			Interval:          time.Duration(cfg.Worker.PriceSyncMinutes) * time.Minute,
			MetadataTargets:   parseMetadataTargets(cfg.Worker.MetadataTargets),
			PriceClients:      priceClients,
			RequestsPerSecond: cfg.Worker.RequestsPerSecond,
			RequestBurst:      cfg.Worker.RequestBurst,
			RetryAttempts:     cfg.Worker.RetryAttempts,
			RetryBaseDelay:    time.Duration(cfg.Worker.RetryBaseDelayMS) * time.Millisecond,
			CircuitFailures:   cfg.Worker.CircuitFailures,
			CircuitCooldown:   time.Duration(cfg.Worker.CircuitCooldownSec) * time.Second,
			MaxPriceRatio:     cfg.Worker.MaxPriceRatio,
			HistoryRetention:  time.Duration(cfg.Worker.HistoryRetentionDays) * 24 * time.Hour,
			FailureSink:       failureSink,
			Lease:             worker.NewPostgresAdvisoryLease(database),
		},
	)
}

func parseMetadataTargets(value string) []worker.MetadataTarget {
	parts := strings.Split(value, ",")
	targets := make([]worker.MetadataTarget, 0, len(parts))
	for _, part := range parts {
		game, language, found := strings.Cut(strings.TrimSpace(part), ":")
		game = models.NormalizeGame(game)
		language = strings.ToLower(strings.TrimSpace(language))
		if !found || game == "" || language == "" {
			continue
		}
		targets = append(targets, worker.MetadataTarget{Game: game, Language: language})
	}
	return targets
}

func validateRuntimeConfig(cfg *config.Config) error {
	if cfg.Scan.TimeoutSeconds < 1 {
		return fmt.Errorf("SCAN_TIMEOUT_SECONDS must be positive")
	}
	if cfg.Worker.PriceSyncMinutes < 1 {
		return fmt.Errorf("PRICE_SYNC_INTERVAL_MINUTES must be positive")
	}
	if cfg.Catalog.LegacyMetadataSync && len(parseMetadataTargets(cfg.Worker.MetadataTargets)) == 0 {
		return fmt.Errorf("METADATA_TARGETS must contain game:language entries")
	}
	return nil
}
