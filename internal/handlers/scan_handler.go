package handlers

import (
	"context"
	"errors"
	"mime"
	"net/http"

	"pokget/internal/auth"
	"pokget/internal/service"
)

const maxScanRequestBytes int64 = 10 << 20

// APIScan accepts a bounded card image upload and validates the envelope before
// the image detector allocates multipart/image buffers.
func (h *Handler) APIScan(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	input := "invalid"
	if err == nil && mediaType == "multipart/form-data" {
		input = "server_image"
	}
	loggedWriter, loggedRequest, finish := beginScanLog(writer, request, input)
	defer finish()
	writer, request = loggedWriter, loggedRequest
	if request.ContentLength > maxScanRequestBytes {
		http.Error(writer, "Card image is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil || mediaType != "multipart/form-data" {
		http.Error(writer, "Card scan requires multipart form data", http.StatusUnsupportedMediaType)
		return
	}

	// One scan per collector at a time, so a single account cannot hold every
	// detector slot.
	if userID, ok := request.Context().Value(auth.UserContextKey{}).(string); ok && userID != "" {
		if _, busy := h.scansInFlight.LoadOrStore(userID, struct{}{}); busy {
			writer.Header().Set("Retry-After", "1")
			http.Error(writer, "Scanner is busy; retry shortly", http.StatusTooManyRequests)
			return
		}
		defer h.scansInFlight.Delete(userID)
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxScanRequestBytes)
	h.executeScan(writer, request)
}

func writeDetectionError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Detection failed"
	var inputError *service.ImageInputError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusRequestTimeout
		message = "Scan timed out"
	case errors.Is(err, service.ErrNoEligibleCards):
		status = http.StatusUnprocessableEntity
		message = "No cards are available for the selected TCG and language"
	case errors.Is(err, service.ErrDetectionUnavailable):
		status = http.StatusServiceUnavailable
		message = "Card recognition is not configured on this server"
	case errors.Is(err, service.ErrInvalidDetectionRequest), errors.As(err, &inputError):
		status = http.StatusBadRequest
		message = "Invalid scan request"
	}
	http.Error(writer, message, status)
}
