package handlers

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"pokget/internal/service"
)

var scanIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{16,64}$`)

type scanResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *scanResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *scanResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *scanResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func beginScanLog(w http.ResponseWriter, r *http.Request, input string) (*scanResponseWriter, *http.Request, func()) {
	id := r.Header.Get("X-Scan-ID")
	if !scanIDPattern.MatchString(id) {
		id = rand.Text()
	}
	logger := slog.Default().With("scan_id", id, "input", input)
	logger.Info("Scan request received")
	w.Header().Set("X-Scan-ID", id)
	response := &scanResponseWriter{ResponseWriter: w}
	started := time.Now()
	return response, r.WithContext(service.WithScanLogger(r.Context(), logger)), func() {
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("Scan request finished", "http_status", status, "duration_ms", time.Since(started).Milliseconds())
	}
}
