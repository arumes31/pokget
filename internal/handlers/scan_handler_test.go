package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIScanRejectsNonMultipartBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader("not an image form"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	new(Handler).APIScan(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestAPIScanRejectsKnownOversizedBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader("body"))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	request.ContentLength = maxScanRequestBytes + 1
	response := httptest.NewRecorder()
	new(Handler).APIScan(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}
