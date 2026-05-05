package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"image"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"io"
	"log/slog"
	"net/http"
	"strings"

	"gettos/internal/models"
	"gettos/internal/service"
)

type Handler struct {
	Templates   *template.Template
	MockCards   []models.Card
	Fingerprint *service.FingerprintService
}

func (h *Handler) Index(w http.ResponseWriter, _ *http.Request) {
	data := map[string]interface{}{
		"Portfolio": h.MockCards,
		"Currency":  "USD",
	}
	if err := h.Templates.ExecuteTemplate(w, "index.html", data); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "USD"
	}

	// userID := r.Context().Value(auth.UserContextKey{}).(string)

	// Fetch Set Completion Data
	type SetProgress struct {
		Name       string
		TotalCards int
		OwnedCards int
		Percent    int
	}
	
	// Mock or DB Query for set completion
	// In a real app, we'd query: 
	// SELECT set_name, count(DISTINCT card_id) as owned, (SELECT count(*) FROM cards c2 WHERE c2.set_name = cards.set_name) as total 
	// FROM portfolio JOIN cards ON portfolio.card_id = cards.id WHERE user_id = $1 GROUP BY set_name
	
	setCompletion := []SetProgress{
		{Name: "151", TotalCards: 165, OwnedCards: 42, Percent: 25},
		{Name: "Paldean Fates", TotalCards: 245, OwnedCards: 180, Percent: 73},
		{Name: "Crown Zenith", TotalCards: 159, OwnedCards: 159, Percent: 100},
	}

	data := map[string]interface{}{
		"Portfolio":     h.MockCards,
		"Currency":      currency,
		"SetCompletion": setCompletion,
	}
	if err := h.Templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Centering(w http.ResponseWriter, _ *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "centering_tool.html", nil); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Auth(w http.ResponseWriter, _ *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "auth.html", nil); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Binders(w http.ResponseWriter, _ *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "binders.html", nil); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Trade(w http.ResponseWriter, _ *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "trade.html", nil); err != nil {
		slog.Error("Template execution failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) APIScan(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 10MB to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	err := r.ParseMultipartForm(10 << 20) // #nosec G120 - bounded by MaxBytesReader
	if err != nil {
		http.Error(w, "Failed to parse form or file too large", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("card_image")
	if err != nil {
		http.Error(w, "Failed to get image from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	lang := r.FormValue("lang")
	if lang == "" {
		lang = "eng"
	}

	imgBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	// 1. Visual Fingerprint Matching (FAST & Language Independent)
	var detectedCard string
	var text string

	if h.Fingerprint != nil {
		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err == nil {
			hash, err := h.Fingerprint.CalculateHash(img)
			if err == nil {
				match, distance, _ := h.Fingerprint.MatchFingerprint(hash)
				if match != nil {
					slog.Info("Fingerprint: Found match", "name", match.Name, "distance", distance)
					detectedCard = match.Name
				}
			}
		}
	}

	// 2. OCR Fallback (if visual matching fails)
	if detectedCard == "" {
		text, detectedCard, err = service.ProcessCardScan(imgBytes, h.MockCards, lang)
		if err != nil {
			slog.Error("OCR: Failed to process scan", "error", err)
			http.Error(w, "Detection failed", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":     strings.ReplaceAll(text, "\n", " "),
		"detected": detectedCard,
	}); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}
