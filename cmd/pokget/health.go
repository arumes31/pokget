package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

func registerHealthRoutes(router *mux.Router, database *sql.DB) {
	router.HandleFunc("/health/live", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodGet, http.MethodHead)
	router.HandleFunc("/health/ready", databaseReadinessHandler(database)).Methods(http.MethodGet, http.MethodHead)
}

func databaseReadinessHandler(database *sql.DB) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if database == nil {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		// Catalog syncs and price backfills can hold the pool for several
		// seconds; a short ping timeout made readiness flap to 503 while the
		// database was healthy but busy. The compose healthcheck allows 15 s.
		ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
		defer cancel()
		if err := database.PingContext(ctx); err != nil {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}
