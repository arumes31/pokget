// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"pokget/internal/catalog"
	"pokget/internal/catalog/source"
	"pokget/internal/config"
	"pokget/internal/db"
	"pokget/internal/handlers"
	"pokget/internal/models"
	"pokget/internal/service"
	"pokget/internal/worker"
)

// appServices groups the services and background workers built from the
// database connection during startup.
type appServices struct {
	fingerprintSvc     *service.FingerprintService
	auditSvc           *service.AuditService
	dataWorker         *worker.DataSyncWorker
	catalogRepository  *catalog.PostgresRepository
	catalogWorker      *worker.CatalogWorker
	catalogImageWorker *worker.CatalogImageWorker
}

// initServices builds the services and workers that depend on the database
// connection. Fatal initialization failures are returned as errors; the
// caller decides how to report and exit.
func initServices(cfg *config.Config, database *sql.DB, fingerprintIndexDirty *atomic.Bool) (*appServices, error) {
	services := &appServices{}
	if database == nil {
		return services, nil
	}

	fingerprintSvc := service.NewFingerprintService(database)
	// SCAN-02: Apply configurable pHash thresholds from config
	fingerprintSvc.PhashHighConf = cfg.Scan.PhashHighConf
	fingerprintSvc.PhashPotential = cfg.Scan.PhashPotential
	// SCAN-03: Set OCR pool size from config
	service.OCRPoolSize = cfg.Scan.OCRPoolSize
	services.fingerprintSvc = fingerprintSvc
	services.auditSvc = service.NewAuditService(database)

	if err := db.SeedDatabase(database); err != nil {
		slog.Error("Database seeding failed", "error", err)
	}

	dataWorker, err := newDataSyncWorker(database, cfg, fingerprintSvc)
	if err != nil {
		return nil, fmt.Errorf("data sync worker initialization failed: %w", err)
	}
	services.dataWorker = dataWorker

	if cfg.Catalog.Enabled || cfg.Catalog.ImagesEnabled {
		catalogRepository, err := catalog.NewPostgresRepository(database)
		if err != nil {
			return nil, fmt.Errorf("catalog initialization failed: %w", err)
		}
		services.catalogRepository = catalogRepository
		if cfg.Catalog.Enabled {
			httpOptions := source.HTTPOptions{
				Client:       &http.Client{Timeout: 5 * time.Minute},
				UserAgent:    "pokget-catalog/1.0",
				MaxBodyBytes: 512 << 20,
				RequestDelay: time.Duration(cfg.Catalog.RequestDelayMS) * time.Millisecond,
			}
			providers := source.DefaultProviders(httpOptions, cfg.Catalog.Language, cfg.Catalog.WeissMaxPages)
			services.catalogWorker = worker.NewCatalogWorker(
				catalogRepository,
				providers,
				cfg.Catalog.BatchSize,
				time.Duration(cfg.Catalog.SyncIntervalMins)*time.Minute,
			)
		}
		if cfg.Catalog.ImagesEnabled {
			processor, err := catalog.NewImageProcessor(catalog.ImageProcessorConfig{
				StoreDir:     cfg.Catalog.ImageStore,
				AllowedHosts: source.DefaultImageHosts(),
			})
			if err != nil {
				return nil, fmt.Errorf("catalog image processor initialization failed: %w", err)
			}
			catalogImageWorker, err := worker.NewCatalogImageWorker(
				catalogRepository,
				processor,
				worker.CatalogImageWorkerConfig{
					Owner:         fmt.Sprintf("pokget:%d", os.Getpid()),
					BatchSize:     cfg.Catalog.ImageBatchSize,
					PollInterval:  time.Duration(cfg.Catalog.ImagePollIntervalMS) * time.Millisecond,
					LeaseDuration: 2 * time.Minute,
					OnChanged: func(count int) {
						fingerprintIndexDirty.Store(true)
						slog.Debug("Catalog image batch changed", "processed", count)
					},
				},
			)
			if err != nil {
				return nil, fmt.Errorf("catalog image worker initialization failed: %w", err)
			}
			services.catalogImageWorker = catalogImageWorker
		}
	}
	return services, nil
}

// loadCardsCache fetches all cards from the database for handlers (caching in
// memory for fast scanning).
func loadCardsCache(database *sql.DB) []models.Card {
	var allCards []models.Card
	if database != nil {
		rows, err := database.Query("SELECT id, name, set_name, COALESCE(price_usd, 0), COALESCE(price_eur, 0), COALESCE(image_url, ''), COALESCE(variant, ''), COALESCE(change_24h, 0), phash, COALESCE(game, ''), COALESCE(language, ''), COALESCE(rarity, ''), COALESCE(set_code, ''), COALESCE(collector_number, ''), catalog_active FROM cards WHERE superseded_by_card_id IS NULL AND (source_id IS NULL OR catalog_active = TRUE)")
		if err != nil {
			slog.Error("Database: Failed to load cards cache", "error", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var c models.Card
				if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &c.Phash, &c.Game, &c.Language, &c.Rarity, &c.SetCode, &c.CollectorNumber, &c.CatalogActive); err == nil {
					allCards = append(allCards, c)
				}
			}
			if err := rows.Err(); err != nil {
				slog.Error("Database: Failed while loading cards cache", "error", err)
			}
			slog.Info("Database: Loaded cards into cache", "count", len(allCards))
		}
	}
	return allCards
}

// templateFuncMap returns the helper functions available to HTML templates.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
	}
}

// newCryptoService derives the AES-256 key from the session key and builds
// the crypto service used for encrypting sensitive fields.
func newCryptoService(sessionKey string) (*service.CryptoService, error) {
	cryptoKey := deriveKey(sessionKey, "pokget:crypto:aes256")
	return service.NewCryptoService(string(cryptoKey))
}

// detectBuildVersion versions static assets from the compiled CSS mtime.
func detectBuildVersion() string {
	buildVersion := "1"
	if info, err := os.Stat("static/css/tailwind.css"); err == nil {
		buildVersion = fmt.Sprintf("%d", info.ModTime().Unix())
	}
	return buildVersion
}

// newHandler assembles the HTTP handler with its service dependencies.
func newHandler(
	cfg *config.Config,
	database *sql.DB,
	templates *template.Template,
	allCards []models.Card,
	services *appServices,
	detectionPipeline *service.DetectionPipeline,
	cryptoSvc *service.CryptoService,
	llmSvc *service.LLMService,
	buildVersion string,
) *handlers.Handler {
	return &handlers.Handler{
		Templates:     templates,
		MockCards:     allCards,
		Fingerprint:   services.fingerprintSvc, // BUG-H01: Reuse fingerprintSvc instead of creating new one
		Detection:     detectionPipeline,
		Audit:         services.auditSvc,
		Crypto:        cryptoSvc,
		Game:          service.NewGamificationService(database),
		LLM:           llmSvc,
		PriceClient:   service.NewScraperPriceClient(),
		DB:            database,
		BuildVersion:  buildVersion,
		ScanTimeout:   time.Duration(cfg.Scan.TimeoutSeconds) * time.Second,
		SecureCookies: cfg.App.SecureCookies, // BUG-C03: Wire up configurable Secure flag
	}
}

// startBackgroundWorkers launches the LLM auto-setup, data sync, catalog
// sync, and catalog image workers. All workers stop when workerCtx is
// canceled and are tracked by backgroundWorkers.
func startBackgroundWorkers(
	workerCtx context.Context,
	llmSvc *service.LLMService,
	services *appServices,
	h *handlers.Handler,
	backgroundWorkers *sync.WaitGroup,
	fingerprintIndexDirty *atomic.Bool,
) {
	backgroundWorkers.Add(1)
	go func() {
		defer backgroundWorkers.Done()
		llmSvc.AutoSetupContext(workerCtx)
	}()

	// Wire up background sync callbacks to reload cache and rebuild the BK-tree dynamically.
	reloadCards := func(source string) {
		slog.Info("Worker: catalog changed, reloading cards cache and rebuilding BK-tree", "source", source)
		if count, err := h.ReloadCardsCache(); err != nil {
			slog.Error("Worker: Failed to reload cards cache", "source", source, "error", err)
		} else {
			slog.Info("Worker: Successfully reloaded cache", "source", source, "count", count)
		}
	}
	if services.dataWorker != nil {
		services.dataWorker.OnSyncComplete = func() {
			reloadCards("legacy")
		}
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			services.dataWorker.Start(workerCtx)
		}()
	}
	if services.catalogWorker != nil {
		services.catalogWorker.OnChanged = func() { reloadCards("catalog") }
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			services.catalogWorker.Start(workerCtx)
		}()
	}
	if services.catalogImageWorker != nil {
		progressReporter := newFingerprintProgressReporter(services.catalogRepository, slog.Default())
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			if err := services.catalogImageWorker.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Catalog image worker stopped", "error", err)
			}
		}()
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			if err := progressReporter.Report(workerCtx); err != nil {
				slog.Warn("Catalog fingerprint progress unavailable", "error", err)
			}
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					if fingerprintIndexDirty.Swap(false) {
						services.fingerprintSvc.RebuildTree()
						slog.Info("Catalog image fingerprints reloaded")
						if err := progressReporter.Report(workerCtx); err != nil {
							slog.Warn("Catalog fingerprint progress unavailable", "error", err)
						}
					}
				}
			}
		}()
	}
}
