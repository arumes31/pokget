package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScanLogsBoundClientDiagnosticsAndCorrelateScope(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	request := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader(`{"ocr_text":"PRIVATE OCR TEXT","game":"pokemon","lang":"deu"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Scan-ID", "scan-1234567890123456")
	request.Header.Set("X-Device-OCR", "usable")
	request.Header.Set("X-Device-OCR-MS", "1234")
	response := httptest.NewRecorder()
	new(Handler).APIScan(response, request)
	if response.Code != 422 {
		t.Fatalf("status: %d", response.Code)
	}
	for _, field := range []string{`"scan_id":"scan-1234567890123456"`, `"input":"device_text"`, `"client_device_ocr":"usable"`, `"client_device_ocr_ms":1234`, `"game":"pokemon"`, `"language":"de"`, `"eligible_cards":0`, `"http_status":422`} {
		if !strings.Contains(output.String(), field) {
			t.Errorf("missing %s in %s", field, &output)
		}
	}
	if response.Header().Get("X-Scan-ID") != "scan-1234567890123456" {
		t.Error("missing response scan ID")
	}
	if strings.Contains(output.String(), "PRIVATE") {
		t.Error("OCR text leaked into logs")
	}
	output.Reset()
	request = httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	request.Header.Set("X-Scan-ID", "PRIVATE\nforged")
	request.Header.Set("X-Device-OCR", "PRIVATE")
	request.Header.Set("X-Device-OCR-MS", "-10")
	request.Header.Set("X-Scan-Fallback", "PRIVATE")
	response = httptest.NewRecorder()
	new(Handler).APIScan(response, request)
	if strings.Contains(output.String(), "PRIVATE") || strings.Contains(output.String(), `"client_device_ocr_ms":-10`) {
		t.Fatal("untrusted header leaked")
	}
	if len(response.Header().Get("X-Scan-ID")) < 16 {
		t.Error("invalid ID was not replaced")
	}
}
