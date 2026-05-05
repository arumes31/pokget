package main

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"time"

	"gettos/internal/auth"
	"gettos/internal/db"
	"gettos/internal/handlers"
	"gettos/internal/models"
	"gettos/internal/service"
	"gettos/internal/worker"

	"github.com/gorilla/mux"
)

func main() {
	// Initialize Structured Logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Initialize Database
	db.InitDB()
	if err := db.SeedDatabase(db.DB); err != nil {
		slog.Error("Database seeding failed", "error", err)
	}

	// Start Price Sync Worker after DB is ready
	priceClient := &service.DefaultPriceClient{Scraper: &service.ScraperPriceClient{}}
	priceWorker := worker.NewPriceSyncWorker(db.DB, priceClient, 1*time.Hour)
	go priceWorker.Start(context.Background())

	// Fetch cards from DB for handlers
	var mockCards []models.Card
	rows, err := db.DB.Query("SELECT id, name, set_name, price_usd, price_eur, image_url FROM cards LIMIT 50")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var c models.Card
			if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL); err != nil {
				slog.Error("Failed to scan card row", "error", err)
				continue
			}
			mockCards = append(mockCards, c)
		}
	}

	// Load Templates
	templates := template.Must(template.ParseGlob("templates/*.html"))

	// Initialize Handlers
	h := &handlers.Handler{
		Templates:   templates,
		MockCards:   mockCards,
		Fingerprint: service.NewFingerprintService(db.DB),
	}

	r := mux.NewRouter()

	// Static files
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public Routes
	r.HandleFunc("/", h.Index).Methods("GET")
	r.HandleFunc("/auth", h.Auth).Methods("GET")
	r.HandleFunc("/auth/register", h.Register).Methods("POST")
	r.HandleFunc("/auth/login", h.Login).Methods("POST")
	r.HandleFunc("/auth/confirm", h.ConfirmEmail).Methods("GET")
	r.HandleFunc("/api/scan", h.APIScan).Methods("POST")

	// Protected Routes (Require Authentication)
	protected := r.PathPrefix("/").Subrouter()
	protected.Use(auth.Middleware)
	protected.HandleFunc("/dashboard", h.Dashboard).Methods("GET")
	protected.HandleFunc("/centering", h.Centering).Methods("GET")
	protected.HandleFunc("/binders", h.Binders).Methods("GET")
	protected.HandleFunc("/trade", h.Trade).Methods("GET")
	protected.HandleFunc("/api/portfolio/add", h.AddCardToPortfolio).Methods("POST")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("Server starting", "port", port)
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}
