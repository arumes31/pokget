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
	priceClient := &service.DefaultPriceClient{Scraper: service.NewScraperPriceClient()}
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
	return worker.NewConfiguredDataSyncWorker(
		database,
		priceClient,
		metadataClient,
		metadataService,
		worker.DataSyncConfig{
			Interval:          time.Duration(cfg.Worker.PriceSyncMinutes) * time.Minute,
			MetadataTargets:   parseMetadataTargets(cfg.Worker.MetadataTargets),
			PriceClients:      map[string][]service.PriceClient{"*": {priceClient}},
			RequestsPerSecond: cfg.Worker.RequestsPerSecond,
			RequestBurst:      cfg.Worker.RequestBurst,
			RetryAttempts:     cfg.Worker.RetryAttempts,
			RetryBaseDelay:    time.Duration(cfg.Worker.RetryBaseDelayMS) * time.Millisecond,
			CircuitFailures:   cfg.Worker.CircuitFailures,
			CircuitCooldown:   time.Duration(cfg.Worker.CircuitCooldownSec) * time.Second,
			MaxPriceRatio:     cfg.Worker.MaxPriceRatio,
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

func validateWorkerConfig(cfg *config.Config) error {
	if cfg.Worker.PriceSyncMinutes < 1 {
		return fmt.Errorf("PRICE_SYNC_INTERVAL_MINUTES must be positive")
	}
	if cfg.Catalog.LegacyMetadataSync && len(parseMetadataTargets(cfg.Worker.MetadataTargets)) == 0 {
		return fmt.Errorf("METADATA_TARGETS must contain game:language entries")
	}
	return nil
}
