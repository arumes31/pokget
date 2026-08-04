package handlers

import (
	"context"
	"errors"
	"mime"
	"net/http"

	"pokget/internal/service"
)

const maxScanRequestBytes int64 = 10 << 20

// APIScan validates the HTTP upload envelope before the detector allocates
// multipart or image buffers. Detection orchestration remains in executeScan
// while the request/response contract evolves independently.
func (h *Handler) APIScan(writer http.ResponseWriter, request *http.Request) {
	if request.ContentLength > maxScanRequestBytes {
		http.Error(writer, "Card image is too large", http.StatusRequestEntityTooLarge)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		http.Error(writer, "Card scan requires multipart form data", http.StatusUnsupportedMediaType)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxScanRequestBytes)
	h.executeScan(writer, request)
}

func writeDetectionError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Detection failed"
	var inputError *service.OCRInputError
	var unavailableError *service.OCRUnavailableError
	var allPassesError *service.OCRAllPassesFailedError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusRequestTimeout
		message = "Scan timed out"
	case errors.Is(err, service.ErrNoEligibleCards):
		status = http.StatusUnprocessableEntity
		message = "No cards are available for the selected TCG and language"
	case errors.Is(err, service.ErrInvalidDetectionRequest), errors.As(err, &inputError):
		status = http.StatusBadRequest
		message = "Invalid scan request"
	case errors.As(err, &unavailableError):
		status = http.StatusServiceUnavailable
		message = "OCR is temporarily unavailable"
	case errors.As(err, &allPassesError):
		status = http.StatusUnprocessableEntity
		message = "The card image could not be read"
	}
	http.Error(writer, message, status)
}
