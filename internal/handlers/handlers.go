package handlers

import (
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"gettos/internal/models"
	"gettos/internal/service"
)

type Handler struct {
	Templates *template.Template
	MockCards []models.Card
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

	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Failed to get image", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read file into bytes
	imgBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	text, detectedCard, err := service.ProcessCardScan(imgBytes, h.MockCards)
	if err != nil {
		http.Error(w, "OCR failed", http.StatusInternalServerError)
		return
	}

	// Calculate Auto-Snap bounds
	bounds, err := service.DetectCardEdges(imgBytes)
	if err != nil {
		slog.Error("Edge detection failed", "error", err)
		// We can still return the OCR result even if edge detection fails
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":     strings.ReplaceAll(text, "\n", " "),
		"detected": detectedCard,
		"bounds":   bounds,
	}); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}
