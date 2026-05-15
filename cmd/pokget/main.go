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

	// Initialize Database
	db.InitDB()
	var priceWorker *worker.PriceSyncWorker
	// Apply Migrations
	if err := db.ApplyMigrations(db.DB, cfg.DB.MigrationsPath); err != nil {
		slog.Error("Migration error", "error", err)
		os.Exit(1)
	}

	if db.DB != nil {
		if err := db.SeedDatabase(db.DB); err != nil {
			slog.Error("Database seeding failed", "error", err)
		}

		// Start Price Sync Worker after DB is ready
		priceClient := &service.DefaultPriceClient{Scraper: &service.ScraperPriceClient{}}
		priceWorker = worker.NewPriceSyncWorker(db.DB, priceClient, 1*time.Hour)
		go priceWorker.Start(context.Background())
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
	auditSvc := service.NewAuditService(db.DB)
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

	// Initialize Handlers
	h := &handlers.Handler{
		Templates:    templates,
		MockCards:    allCards,
		Fingerprint:  service.NewFingerprintService(db.DB),
		Audit:        auditSvc,
		Crypto:       cryptoSvc,
		Game:         service.NewGamificationService(db.DB),
		DB:           db.DB,
		BuildVersion: buildVersion,
	}

	r := mux.NewRouter()
	r.Use(middleware.LoggingMiddleware)
	r.Use(auth.RateLimitMiddleware)
	r.Use(auth.ProxyMiddleware)
	
	// CSRF Protection
	csrfMiddleware := csrf.Protect(
		[]byte(cfg.Auth.SessionKey),
		csrf.Secure(false), // Disable for local development without HTTPS
	)
	r.Use(csrfMiddleware)

	// Static files
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public Routes
	r.HandleFunc("/", h.Index).Methods("GET")
	r.HandleFunc("/auth", h.Auth).Methods("GET")
	r.HandleFunc("/auth/register", h.Register).Methods("POST")
	r.HandleFunc("/auth/login", h.Login).Methods("POST")
	r.HandleFunc("/auth/resend", h.ResendVerification).Methods("POST")
	r.HandleFunc("/auth/confirm", h.ConfirmEmail).Methods("GET")
	r.HandleFunc("/auth/confirm", h.ProcessConfirmEmail).Methods("POST")
	r.HandleFunc("/auth/logout", h.Logout).Methods("GET", "POST")
	r.HandleFunc("/api/scan", h.APIScan).Methods("POST")
	r.HandleFunc("/vault/{slug}", h.PublicVault).Methods("GET")
	r.HandleFunc("/errors", h.ErrorDatabase).Methods("GET")

	// Protected Routes (Require Authentication)
	protected := r.PathPrefix("/").Subrouter()
	protected.Use(auth.Middleware)
	protected.HandleFunc("/dashboard", h.Dashboard).Methods("GET")
	protected.HandleFunc("/centering", h.Centering).Methods("GET")
	protected.HandleFunc("/binders", h.Binders).Methods("GET")
	protected.HandleFunc("/binders/create", h.CreateBinder).Methods("POST")
	protected.HandleFunc("/binders/{id}", h.BinderDetail).Methods("GET")
	protected.HandleFunc("/trade", h.Trade).Methods("GET")
	protected.HandleFunc("/portfolio/add", h.AddCardToPortfolio).Methods("POST")
	protected.HandleFunc("/portfolio/edit", h.EditPortfolioItem).Methods("POST")
	protected.HandleFunc("/portfolio/toggle-visibility", h.ToggleVisibility).Methods("POST")
	protected.HandleFunc("/wantlist", h.Wantlist).Methods("GET")
	protected.HandleFunc("/wantlist/add", h.AddToWantlist).Methods("POST")
	protected.HandleFunc("/errors/submit", h.SubmitError).Methods("POST")
	protected.HandleFunc("/api/gamification/heartbeat", h.Heartbeat).Methods("POST")
	protected.HandleFunc("/api/portfolio/add", h.AddCardToPortfolio).Methods("POST")

	// Admin Routes (Require Authentication + Admin Role)
	admin := r.PathPrefix("/api/admin").Subrouter()
	admin.Use(auth.Middleware)
	admin.Use(auth.AdminMiddleware(db.DB))
	admin.HandleFunc("/refresh-cache", h.RefreshCache).Methods("POST")

	slog.Info("Server starting", "port", cfg.App.Port)
	srv := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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
	if priceWorker != nil {
		priceWorker.Stop()
	}

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exiting")
}
