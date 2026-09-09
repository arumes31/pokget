package handlers

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"pokget/internal/service"
)

var scanIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{16,64}$`)

// Client diagnostics are untrusted observations, never detection inputs.
func scanDiagnostic(value string) string {
	switch value {
	case "usable", "weak_text", "timeout", "error", "paused", "cancelled", "disabled", "unsupported", "server_only", "none", "no_match", "unsupported_server":
		return value
	default:
		return "unknown"
	}
}

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

func beginScanLog(w http.ResponseWriter, r *http.Request, input string) (*scanResponseWriter, *http.Request, func()) {
	id := r.Header.Get("X-Scan-ID")
	if !scanIDPattern.MatchString(id) {
		id = rand.Text()
	}
	logger := slog.Default().With("scan_id", id, "input", input)
	fields := []any{"client_device_ocr", scanDiagnostic(r.Header.Get("X-Device-OCR")), "client_fallback", scanDiagnostic(r.Header.Get("X-Scan-Fallback"))}
	if elapsed, err := strconv.Atoi(r.Header.Get("X-Device-OCR-MS")); err == nil && elapsed >= 0 && elapsed <= 60000 {
		fields = append(fields, "client_device_ocr_ms", elapsed)
	}
	logger.Info("Scan request received", fields...)
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
