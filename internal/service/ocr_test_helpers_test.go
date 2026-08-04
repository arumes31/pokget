package service

import (
	"errors"
	"testing"
)

func requireOCRSuccessOrUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var unavailable *OCRUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("unexpected OCR error: %v", err)
	}
}
