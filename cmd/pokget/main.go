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
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"pokget/internal/auth"
	"pokget/internal/catalog"
	"pokget/internal/catalog/source"
	"pokget/internal/config"
	"pokget/internal/db"
	"pokget/internal/handlers"
	"pokget/internal/middleware"
	"pokget/internal/models"
	"pokget/internal/service"
	"pokget/internal/worker"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

// deriveKey derives a purpose-specific key from a master key using HMAC-SHA256
func deriveKey(masterKey, purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(masterKey))
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

func newCSRFMiddleware(key []byte, secure bool) func(http.Handler) http.Handler {
	protect := csrf.Protect(key, csrf.Secure(secure))
	if secure {
		return protect
	}
	return func(next http.Handler) http.Handler {
		protected := protect(next)
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			protected.ServeHTTP(writer, csrf.PlaintextHTTPRequest(request))
		})
	}
}

func useGlobalMiddleware(router *mux.Router) {
	router.Use(middleware.LoggingMiddleware)
	router.Use(middleware.SecurityHeadersMiddleware)
	// Resolve a trusted proxy address before selecting a per-client limiter.
	router.Use(auth.ProxyMiddleware)
	router.Use(auth.RateLimitMiddleware)
}

func registerScanRoute(
	router *mux.Router,
	database *sql.DB,
	csrfMiddleware func(http.Handler) http.Handler,
	maxConcurrent int,
	handler http.Handler,
) {
	scanRouter := router.PathPrefix("/api").Subrouter()
	scanRouter.Use(auth.APIAuthMiddleware(database))
	scanRouter.Use(auth.ScanRateLimitMiddleware)
	scanRouter.Use(middleware.ConcurrentLimitMiddleware(maxConcurrent))
	scanRouter.Use(middleware.MaxBytesMiddlewareWithLimit(20 << 20))
	scanRouter.Use(csrfMiddleware)
	scanRouter.Handle("/scan", handler).Methods(http.MethodPost)
}

func canonicalFragmentRedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("HX-Request") == "true" {
			next.ServeHTTP(w, r)
			return
		}

		view := ""
		values := url.Values{}
		switch r.URL.Path {
		case "/dashboard":
			view = "home"
		case "/wantlist":
			view = "wantlist"
		case "/binders":
			view = "binders"
		case "/centering":
			view = "scan"
		case "/trade":
			view = "trade"
		case "/settings":
			view = "settings"
		default:
			if binderID := mux.Vars(r)["id"]; binderID != "" && strings.HasPrefix(r.URL.Path, "/binders/") {
				view = "binders"
				values.Set("binder", binderID)
			}
		}
		if view == "" {
			next.ServeHTTP(w, r)
			return
		}

		values.Set("view", view)
		http.Redirect(w, r, "/?"+values.Encode(), http.StatusSeeOther)
	})
}

func main() {
	// Load Configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}
	// Initialize Structured Logger
	logLevel := slog.LevelInfo
	if cfg.App.Debug {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	database, err := db.Connect()
	if err != nil {
		slog.Error("Database connection failed", "error", err)
		os.Exit(1)
	}
	db.DB = database
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("Failed to close database connection", "error", err)
		}
	}()

	// Initialize Services
	var fingerprintSvc *service.FingerprintService
	var auditSvc *service.AuditService

	var dataWorker *worker.DataSyncWorker
	var catalogWorker *worker.CatalogWorker
	var catalogImageWorker *worker.CatalogImageWorker
	var backgroundWorkers sync.WaitGroup
	var fingerprintIndexDirty atomic.Bool
	workerCtx, workerCancel := context.WithCancel(context.Background())
	// Apply Migrations
	if err := db.ApplyMigrations(database, cfg.DB.MigrationsPath); err != nil {
		slog.Error("Migration error", "error", err)
		os.Exit(1)
	}

	if db.DB != nil {
		fingerprintSvc = service.NewFingerprintService(db.DB)
		// SCAN-02: Apply configurable pHash thresholds from config
		fingerprintSvc.PhashHighConf = cfg.Scan.PhashHighConf
		fingerprintSvc.PhashPotential = cfg.Scan.PhashPotential
		// SCAN-03: Set OCR pool size from config
		service.OCRPoolSize = cfg.Scan.OCRPoolSize
		auditSvc = service.NewAuditService(db.DB)

		if err := db.SeedDatabase(db.DB); err != nil {
			slog.Error("Database seeding failed", "error", err)
		}

		// Start Data Sync Worker after DB is ready
		scraperPriceClient := service.NewScraperPriceClient()
		priceClient := &service.DefaultPriceClient{Scraper: scraperPriceClient}
		var metadataClient service.MetadataClient
		var metadataSvc *service.MetadataService
		if !cfg.Catalog.Enabled && cfg.Catalog.LegacyMetadataSync {
			metadataClient = service.NewTCGDexClient()
			metadataSvc = service.NewMetadataService(fingerprintSvc)
		}

		dataWorker = worker.NewDataSyncWorker(db.DB, priceClient, metadataClient, metadataSvc, 1*time.Hour)

		if cfg.Catalog.Enabled || cfg.Catalog.ImagesEnabled {
			catalogRepository, err := catalog.NewPostgresRepository(db.DB)
			if err != nil {
				slog.Error("Catalog initialization failed", "error", err)
				os.Exit(1)
			}
			if cfg.Catalog.Enabled {
				httpOptions := source.HTTPOptions{
					Client:       &http.Client{Timeout: 5 * time.Minute},
					UserAgent:    "pokget-catalog/1.0",
					MaxBodyBytes: 512 << 20,
					RequestDelay: time.Duration(cfg.Catalog.RequestDelayMS) * time.Millisecond,
				}
				providers := source.DefaultProviders(httpOptions, cfg.Catalog.Language, cfg.Catalog.WeissMaxPages)
				catalogWorker = worker.NewCatalogWorker(
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
					slog.Error("Catalog image processor initialization failed", "error", err)
					os.Exit(1)
				}
				catalogImageWorker, err = worker.NewCatalogImageWorker(
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
					slog.Error("Catalog image worker initialization failed", "error", err)
					os.Exit(1)
				}
			}
		}
	}

	// Fetch all cards from DB for handlers (caching in memory for fast scanning)
	var allCards []models.Card
	if db.DB != nil {
		rows, err := db.DB.Query("SELECT id, name, set_name, COALESCE(price_usd, 0), COALESCE(price_eur, 0), COALESCE(image_url, ''), COALESCE(variant, ''), COALESCE(change_24h, 0), phash, COALESCE(game, ''), COALESCE(language, ''), COALESCE(rarity, '') FROM cards WHERE superseded_by_card_id IS NULL")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var c models.Card
				if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &c.Phash, &c.Game, &c.Language, &c.Rarity); err == nil {
					allCards = append(allCards, c)
				}
			}
			if err := rows.Err(); err != nil {
				slog.Error("Database: Failed while loading cards cache", "error", err)
			}
			slog.Info("Database: Loaded cards into cache", "count", len(allCards))
		}
	}

	// Load Templates
	funcMap := template.FuncMap{
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
	templates := template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))

	// Initialize Services
	cryptoKey := deriveKey(cfg.Auth.SessionKey, "pokget:crypto:aes256")
	cryptoSvc, err := service.NewCryptoService(string(cryptoKey))
	if err != nil {
		slog.Error("Failed to initialize crypto service", "error", err)
		os.Exit(1)
	}

	// Versioning for assets
	buildVersion := "1"
	if info, err := os.Stat("static/css/tailwind.css"); err == nil {
		buildVersion = fmt.Sprintf("%d", info.ModTime().Unix())
	}

	// Initialize LLM service
	llmSvc := service.NewLLMService()
	backgroundWorkers.Add(1)
	go func() {
		defer backgroundWorkers.Done()
		llmSvc.AutoSetupContext(workerCtx)
	}()

	// Initialize Detection Pipeline (SCAN-07, SCAN-09, SCAN-16)
	var detectionPipeline *service.DetectionPipeline
	if fingerprintSvc != nil {
		detectionPipeline = service.NewDetectionPipeline(fingerprintSvc, llmSvc)
	}

	// Initialize Handlers
	h := &handlers.Handler{
		Templates:     templates,
		MockCards:     allCards,
		Fingerprint:   fingerprintSvc, // BUG-H01: Reuse fingerprintSvc instead of creating new one
		Detection:     detectionPipeline,
		Audit:         auditSvc,
		Crypto:        cryptoSvc,
		Game:          service.NewGamificationService(db.DB),
		LLM:           llmSvc,
		PriceClient:   service.NewScraperPriceClient(),
		DB:            db.DB,
		BuildVersion:  buildVersion,
		SecureCookies: cfg.App.SecureCookies, // BUG-C03: Wire up configurable Secure flag
	}

	// Wire up background sync callbacks to reload cache and rebuild the BK-tree dynamically.
	reloadCards := func(source string) {
		slog.Info("Worker: catalog changed, reloading cards cache and rebuilding BK-tree", "source", source)
		if count, err := h.ReloadCardsCache(); err != nil {
			slog.Error("Worker: Failed to reload cards cache", "source", source, "error", err)
		} else {
			slog.Info("Worker: Successfully reloaded cache", "source", source, "count", count)
		}
	}
	if dataWorker != nil {
		dataWorker.OnSyncComplete = func() {
			reloadCards("legacy")
		}
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			dataWorker.Start(workerCtx)
		}()
	}
	if catalogWorker != nil {
		catalogWorker.OnChanged = func() { reloadCards("catalog") }
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			catalogWorker.Start(workerCtx)
		}()
	}
	if catalogImageWorker != nil {
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			if err := catalogImageWorker.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Catalog image worker stopped", "error", err)
			}
		}()
		backgroundWorkers.Add(1)
		go func() {
			defer backgroundWorkers.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					if fingerprintIndexDirty.Swap(false) {
						fingerprintSvc.RebuildTree()
						slog.Info("Catalog image fingerprints reloaded")
					}
				}
			}
		}()
	}

	r := mux.NewRouter()
	useGlobalMiddleware(r)

	// CSRF Protection
	csrfKey := deriveKey(cfg.Auth.SessionKey, "pokget:csrf:auth")
	csrfMiddleware := newCSRFMiddleware(csrfKey, cfg.App.SecureCookies)

	// Static files (Exempt from CSRF)
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	registerScanRoute(
		r,
		db.DB,
		csrfMiddleware,
		cfg.Scan.OCRPoolSize,
		http.HandlerFunc(h.APIScan),
	)

	// Web Routes (Protected by CSRF + 1MB MaxBytes limit)
	web := r.NewRoute().Subrouter()
	web.Use(middleware.MaxBytesMiddleware) // 1MB limit for form submissions
	web.Use(csrfMiddleware)

	// Public Web Routes
	web.Handle("/", auth.Middleware(db.DB)(http.HandlerFunc(h.Index))).Methods("GET")
	web.HandleFunc("/auth", h.Auth).Methods("GET")
	web.HandleFunc("/auth/register", h.Register).Methods("POST")
	web.HandleFunc("/auth/login", h.Login).Methods("POST")
	web.HandleFunc("/auth/resend", h.ResendVerification).Methods("POST")
	web.HandleFunc("/auth/confirm", h.ConfirmEmail).Methods("GET")
	web.HandleFunc("/auth/confirm", h.ProcessConfirmEmail).Methods("POST")
	web.HandleFunc("/auth/logout", h.Logout).Methods("POST")
	web.HandleFunc("/vault/{slug}", h.PublicVault).Methods("GET")
	web.HandleFunc("/errors", h.ErrorDatabase).Methods("GET")

	// Protected Routes (Require Authentication + CSRF)
	protected := web.PathPrefix("/").Subrouter()
	protected.Use(auth.Middleware(db.DB))
	protected.Use(canonicalFragmentRedirectMiddleware)
	protected.HandleFunc("/dashboard", h.Dashboard).Methods("GET")
	protected.HandleFunc("/centering", h.Centering).Methods("GET")
	protected.HandleFunc("/binders", h.Binders).Methods("GET")
	protected.HandleFunc("/binders/create", h.CreateBinder).Methods("POST")
	protected.HandleFunc("/binders/auto-name", h.AutoNameBinder).Methods("POST")
	protected.HandleFunc("/binders/{id}", h.BinderDetail).Methods("GET")
	protected.HandleFunc("/trade", h.Trade).Methods("GET")
	protected.HandleFunc("/settings", h.Settings).Methods("GET", "POST")
	protected.HandleFunc("/settings/change-password", h.ChangePassword).Methods("POST") // BUG-M11: Route for password change with session invalidation
	protected.HandleFunc("/settings/public-profile", h.UpdatePublicProfile).Methods("POST")
	protected.HandleFunc("/portfolio/add", h.AddCardToPortfolio).Methods("POST")
	protected.HandleFunc("/portfolio/edit", h.EditPortfolioItem).Methods("POST")
	protected.HandleFunc("/portfolio/delete", h.DeletePortfolioItem).Methods("POST", "DELETE") // BUG-H02: Delete with ownership check
	protected.HandleFunc("/portfolio/binder", h.UpdatePortfolioBinder).Methods("POST")
	protected.HandleFunc("/portfolio/toggle-visibility", h.ToggleVisibility).Methods("POST")
	protected.HandleFunc("/wantlist", h.Wantlist).Methods("GET")
	protected.HandleFunc("/wantlist/add", h.AddToWantlist).Methods("POST")
	protected.HandleFunc("/wantlist/edit", h.UpdateWantlistItem).Methods("POST")
	protected.HandleFunc("/wantlist/delete", h.DeleteWantlistItem).Methods("POST", "DELETE")
	protected.HandleFunc("/errors/submit", h.SubmitError).Methods("POST")
	protected.HandleFunc("/api/gamification/heartbeat", h.Heartbeat).Methods("POST")
	protected.HandleFunc("/api/portfolio/add", h.AddCardToPortfolio).Methods("POST")

	// Admin Routes (Require Authentication + Admin Role + CSRF)
	admin := protected.PathPrefix("/api/admin").Subrouter()
	admin.Use(auth.AdminMiddleware(db.DB))
	admin.HandleFunc("/refresh-cache", h.RefreshCache).Methods("POST")

	slog.Info("Server starting", "port", cfg.App.Port)
	// BUG-C05 FIX: Use configurable WriteTimeout (default 120s) instead of
	// hardcoded 15s which killed scan responses mid-stream during OCR+LLM processing.
	writeTimeout := time.Duration(cfg.App.WriteTimeout) * time.Second
	srv := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful Shutdown Logic
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Stop workers
	workerCancel()
	if dataWorker != nil {
		dataWorker.Stop()
	}

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}
	workersDone := make(chan struct{})
	go func() {
		backgroundWorkers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-ctx.Done():
		slog.Warn("Background workers did not stop before shutdown deadline", "error", ctx.Err())
	}
	if auditSvc != nil {
		// Drain queued audit records while the database pool is still open.
		if err := auditSvc.CloseContext(ctx); err != nil {
			slog.Warn("Audit service did not drain before shutdown deadline", "error", err)
		}
	}

	slog.Info("Server exiting")
}
