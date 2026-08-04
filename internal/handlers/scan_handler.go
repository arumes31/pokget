package handlers

import (
	"mime"
	"net/http"
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
