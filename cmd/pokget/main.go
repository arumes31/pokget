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
	"pokget/internal/config"
	"pokget/internal/db"
	"pokget/internal/middleware"
	"pokget/internal/service"

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
	if err := validateRuntimeConfig(cfg); err != nil {
		slog.Error("Invalid worker configuration", "error", err)
		os.Exit(1)
	}
	// Initialize Structured Logger
	logLevel := slog.LevelInfo
	if cfg.App.Debug {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(newLogHandler(os.Stdout, cfg.App.LogFormat, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := db.ConnectWithRetry(startupCtx, 5, time.Second)
	startupCancel()
	if err != nil {
		slog.Error("Database connection failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("Failed to close database connection", "error", err)
		}
	}()

	workerCtx, workerCancel := context.WithCancel(context.Background())
	// Apply Migrations
	if err := db.ApplyMigrations(database, cfg.DB.MigrationsPath); err != nil {
		slog.Error("Migration error", "error", err)
		os.Exit(1)
	}
	schemaCtx, schemaCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.ValidateRuntimeSchema(schemaCtx, database); err != nil {
		schemaCancel()
		slog.Error("Runtime schema validation failed", "error", err)
		os.Exit(1)
	}
	schemaCancel()

	// Initialize Services and Workers
	var fingerprintIndexDirty atomic.Bool
	services, err := initServices(cfg, database, &fingerprintIndexDirty)
	if err != nil {
		slog.Error("Initialization failed", "error", err)
		os.Exit(1)
	}

	// Fetch all cards from DB for handlers (caching in memory for fast scanning)
	allCards := loadCardsCache(database)

	// Load Templates
	templates := template.Must(template.New("").Funcs(templateFuncMap()).ParseGlob("templates/*.html"))

	cryptoSvc, err := newCryptoService(cfg.Auth.SessionKey)
	if err != nil {
		slog.Error("Failed to initialize crypto service", "error", err)
		os.Exit(1)
	}

	// Versioning for assets
	buildVersion := detectBuildVersion()

	// Initialize LLM service
	llmSvc := service.NewLLMService()

	// Initialize Detection Pipeline (SCAN-07, SCAN-09, SCAN-16)
	var detectionPipeline *service.DetectionPipeline
	if services.fingerprintSvc != nil {
		detectionPipeline = service.NewDetectionPipeline(services.fingerprintSvc, llmSvc)
	}

	// Initialize Handlers
	h := newHandler(cfg, database, templates, allCards, services, detectionPipeline, cryptoSvc, llmSvc, buildVersion)

	var backgroundWorkers sync.WaitGroup
	startBackgroundWorkers(workerCtx, llmSvc, services, h, &backgroundWorkers, &fingerprintIndexDirty)

	r := buildRouter(cfg, database, h)

	slog.Info("Server starting", "port", cfg.App.Port)
	srv := newHTTPServer(cfg, r)

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

	gracefulShutdown(srv, workerCancel, services.dataWorker, &backgroundWorkers, services.auditSvc, 10*time.Second)
}
