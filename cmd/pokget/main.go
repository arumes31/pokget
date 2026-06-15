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
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pokget/internal/auth"
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

	// Initialize Services
	var fingerprintSvc *service.FingerprintService
	var auditSvc *service.AuditService

	var dataWorker *worker.DataSyncWorker
	var workerCancel context.CancelFunc
	// Apply Migrations
	if err := db.ApplyMigrations(db.DB, cfg.DB.MigrationsPath); err != nil {
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
		priceClient := &service.DefaultPriceClient{Scraper: &service.ScraperPriceClient{}}
		metadataClient := service.NewTCGDexClient()
		metadataSvc := service.NewMetadataService(fingerprintSvc)

		dataWorker = worker.NewDataSyncWorker(db.DB, priceClient, metadataClient, metadataSvc, 1*time.Hour)
		var workerCtx context.Context
		workerCtx, workerCancel = context.WithCancel(context.Background())
		go dataWorker.Start(workerCtx)
	}

	// Fetch all cards from DB for handlers (caching in memory for fast scanning)
	var allCards []models.Card
	if db.DB != nil {
		rows, err := db.DB.Query("SELECT id, name, set_name, price_usd, price_eur, image_url, variant, change_24h, phash FROM cards")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var c models.Card
				if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &c.Phash); err == nil {
					allCards = append(allCards, c)
				}
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
	cryptoSvc, err := service.NewCryptoService(cfg.Auth.SessionKey)
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
		DB:            db.DB,
		BuildVersion:  buildVersion,
		SecureCookies: cfg.App.SecureCookies, // BUG-C03: Wire up configurable Secure flag
	}

	r := mux.NewRouter()
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.SecurityHeadersMiddleware)
	r.Use(auth.RateLimitMiddleware)
	r.Use(auth.ProxyMiddleware)

	// CSRF Protection
	csrfMiddleware := csrf.Protect(
		[]byte(cfg.Auth.SessionKey),
		csrf.Secure(false), // Disable for local development without HTTPS
	)

	// Static files (Exempt from CSRF)
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// API routes (Exempt from CSRF for testing/external use)
	// BUG-L03 FIX: Apply 20MB MaxBytes limit for /api/scan to allow image uploads
	// while still preventing extremely large payloads.
	scanRouter := r.PathPrefix("/api").Subrouter()
	scanRouter.Use(middleware.MaxBytesMiddlewareWithLimit(20 << 20)) // 20 MB for image uploads
	scanRouter.HandleFunc("/scan", h.APIScan).Methods("POST")

	// Web Routes (Protected by CSRF + 1MB MaxBytes limit)
	web := r.NewRoute().Subrouter()
	web.Use(middleware.MaxBytesMiddleware) // 1MB limit for form submissions
	web.Use(csrfMiddleware)

	// Public Web Routes
	web.HandleFunc("/", h.Index).Methods("GET")
	web.HandleFunc("/auth", h.Auth).Methods("GET")
	web.HandleFunc("/auth/register", h.Register).Methods("POST")
	web.HandleFunc("/auth/login", h.Login).Methods("POST")
	web.HandleFunc("/auth/resend", h.ResendVerification).Methods("POST")
	web.HandleFunc("/auth/confirm", h.ConfirmEmail).Methods("GET")
	web.HandleFunc("/auth/confirm", h.ProcessConfirmEmail).Methods("POST")
	web.HandleFunc("/auth/logout", h.Logout).Methods("GET", "POST")
	web.HandleFunc("/vault/{slug}", h.PublicVault).Methods("GET")
	web.HandleFunc("/errors", h.ErrorDatabase).Methods("GET")

	// Protected Routes (Require Authentication + CSRF)
	protected := web.PathPrefix("/").Subrouter()
	protected.Use(auth.Middleware)
	protected.HandleFunc("/dashboard", h.Dashboard).Methods("GET")
	protected.HandleFunc("/centering", h.Centering).Methods("GET")
	protected.HandleFunc("/binders", h.Binders).Methods("GET")
	protected.HandleFunc("/binders/create", h.CreateBinder).Methods("POST")
	protected.HandleFunc("/binders/{id}", h.BinderDetail).Methods("GET")
	protected.HandleFunc("/trade", h.Trade).Methods("GET")
	protected.HandleFunc("/settings", h.Settings).Methods("GET", "POST")
	protected.HandleFunc("/settings/change-password", h.ChangePassword).Methods("POST") // BUG-M11: Route for password change with session invalidation
	protected.HandleFunc("/portfolio/add", h.AddCardToPortfolio).Methods("POST")
	protected.HandleFunc("/portfolio/edit", h.EditPortfolioItem).Methods("POST")
	protected.HandleFunc("/portfolio/delete", h.DeletePortfolioItem).Methods("POST") // BUG-H02: Delete with ownership check
	protected.HandleFunc("/portfolio/toggle-visibility", h.ToggleVisibility).Methods("POST")
	protected.HandleFunc("/wantlist", h.Wantlist).Methods("GET")
	protected.HandleFunc("/wantlist/add", h.AddToWantlist).Methods("POST")
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
	if dataWorker != nil {
		workerCancel()
		dataWorker.Stop()
	}

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exiting")
}
