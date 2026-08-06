package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/service"
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

func TestWriteDetectionErrorMapsTypedFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: http.StatusRequestTimeout},
		{name: "no eligible cards", err: service.ErrNoEligibleCards, want: http.StatusUnprocessableEntity},
		{name: "invalid request", err: service.ErrInvalidDetectionRequest, want: http.StatusBadRequest},
		{name: "invalid image", err: &service.OCRInputError{Reason: "bad format"}, want: http.StatusBadRequest},
		{name: "unavailable", err: &service.OCRUnavailableError{}, want: http.StatusServiceUnavailable},
		{name: "all passes", err: &service.OCRAllPassesFailedError{Failures: []error{errors.New("failed")}}, want: http.StatusUnprocessableEntity},
		{name: "internal", err: errors.New("database failed"), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeDetectionError(response, test.err)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
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
