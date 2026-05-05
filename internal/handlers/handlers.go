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

	"gettos/internal/auth"
	"gettos/internal/db"
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

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch Set Completion Data from DB
	type SetProgress struct {
		Name       string
		TotalCards int
		OwnedCards int
		Percent    int
	}
	
	rows, err := db.DB.Query(`
		SELECT 
			c.set_name, 
			COUNT(DISTINCT c.id) FILTER (WHERE p.id IS NOT NULL AND p.user_id = $1) as owned_cards,
			COUNT(DISTINCT c.id) as total_cards
		FROM cards c
		LEFT JOIN portfolio p ON c.id = p.card_id
		GROUP BY c.set_name`, userID)
	
	var setCompletion []SetProgress
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s SetProgress
			if err := rows.Scan(&s.Name, &s.OwnedCards, &s.TotalCards); err == nil {
				if s.TotalCards > 0 {
					s.Percent = (s.OwnedCards * 100) / s.TotalCards
				}
				setCompletion = append(setCompletion, s)
			}
		}
	}
	
	// Fallback to mock if DB is empty for demo purposes
	if len(setCompletion) == 0 {
		setCompletion = []SetProgress{
			{Name: "151", TotalCards: 165, OwnedCards: 42, Percent: 25},
			{Name: "Paldean Fates", TotalCards: 245, OwnedCards: 180, Percent: 73},
		}
	}

	data := struct {
		Currency      string
		SetCompletion []SetProgress
		Portfolio     []models.Card
	}{
		Currency:      currency,
		SetCompletion: setCompletion,
		Portfolio:     h.MockCards, // For now use mock, but in real app fetch from DB
	}

	if err := h.Templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		slog.Error("Template execution failed", "error", err)
	}
}

func (h *Handler) AddCardToPortfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cardID := r.FormValue("card_id")
	if cardID == "" {
		http.Error(w, "Missing card_id", http.StatusBadRequest)
		return
	}

	_, err := db.DB.Exec(`
		INSERT INTO portfolio (user_id, card_id, condition, format)
		VALUES ($1, $2, $3, $4)`,
		userID, cardID, "Near Mint", "Raw")
	
	if err != nil {
		slog.Error("Failed to add card to portfolio", "error", err)
		http.Error(w, "Failed to add card", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Card added to collection!"))
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
	var detectedID string
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
					detectedID = match.ID
				}
			}
		}
	}

	// 2. OCR Fallback (if visual matching fails)
	if detectedCard == "" {
		var ocrMatch string
		text, ocrMatch, err = service.ProcessCardScan(imgBytes, h.MockCards, lang)
		if err != nil {
			slog.Error("OCR: Failed to process scan", "error", err)
			http.Error(w, "Detection failed", http.StatusInternalServerError)
			return
		}
		if ocrMatch != "Unknown Card" {
			detectedCard = ocrMatch
			// Find ID for the OCR match
			for _, c := range h.MockCards {
				if c.Name == ocrMatch {
					detectedID = c.ID
					break
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"text":     strings.ReplaceAll(text, "\n", " "),
		"detected": detectedCard,
		"id":       detectedID,
	}); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}
