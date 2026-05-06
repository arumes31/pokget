// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package handlers

import (
	"bytes"
	"database/sql"
	"encoding/base64"
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

	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"

	"github.com/gorilla/csrf"
)

type Handler struct {
	Templates    *template.Template
	MockCards    []models.Card
	Fingerprint  *service.FingerprintService
	Mailer       service.Mailer
	Audit        *service.AuditService
	Crypto       *service.CryptoService
	Game         *service.GamificationService
	DB           *sql.DB
	BuildVersion string
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["CSRFToken"] = csrf.Token(r)
	data["BuildVersion"] = h.BuildVersion
	if err := h.Templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("Template execution failed", "template", name, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Index", "method", r.Method, "url", r.URL.String())
	session, _ := auth.Store.Get(r, "session")
	if userID, ok := session.Values["user_id"].(string); !ok || userID == "" {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	h.render(w, r, "index.html", map[string]interface{}{
		"Portfolio": h.MockCards,
		"Currency":  "USD",
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Dashboard", "method", r.Method, "url", r.URL.String())
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
	
	rows, err := h.DB.Query(`
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

	// Fetch Portfolio with multipliers
	rowsPortfolio, _ := h.DB.Query(`
		SELECT p.id, p.condition, p.custom_price, c.id, c.name, c.set_name, c.image_url, c.price_usd, c.game
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1`, userID)
	
	var portfolio []models.PortfolioItem
	if rowsPortfolio != nil {
		defer rowsPortfolio.Close()
		for rowsPortfolio.Next() {
			var p models.PortfolioItem
			if err := rowsPortfolio.Scan(&p.ID, &p.Condition, &p.CustomPrice, &p.Card.ID, &p.Card.Name, &p.Card.Set, &p.Card.ImageURL, &p.Card.PriceUSD, &p.Card.Game); err == nil {
				portfolio = append(portfolio, p)
			}
		}
	}

	// Calculate Total Valuation with multipliers
	var totalValuation float64
	var multipliers map[string]float64
	var multStr string
	_ = h.DB.QueryRow("SELECT condition_multipliers FROM users WHERE id = $1", userID).Scan(&multStr)
	_ = json.Unmarshal([]byte(multStr), &multipliers)

	priceService := &service.ScraperPriceClient{}
	for _, item := range portfolio {
		if item.CustomPrice > 0 {
			totalValuation += item.CustomPrice
		} else {
			price, _ := item.Card.PriceUSD.Float64()
			totalValuation += priceService.ApplyMultiplier(price, item.Condition, multipliers)
		}
	}
	
	// Fetch User XP and Rank
	var xp int
	var rankTitle string
	_ = h.DB.QueryRow("SELECT xp, rank_title FROM users WHERE id = $1", userID).Scan(&xp, &rankTitle)
	
	rank := h.Game.GetUserRank(xp)
	_, _, xpPercent := h.Game.GetProgressToNextRank(xp)

	h.render(w, r, "dashboard.html", map[string]interface{}{
		"Currency":       currency,
		"TotalValuation": totalValuation,
		"SetCompletion":  setCompletion,
		"Portfolio":      portfolio,
		"XP":             xp,
		"Rank":           rankTitle,
		"RankIcon":       rank.IconURL,
		"XPPercent":      xpPercent,
	})
}

func (h *Handler) AddCardToPortfolio(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: AddCardToPortfolio", "method", r.Method, "url", r.URL.String())
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
	notes := r.FormValue("notes")
	customPrice := r.FormValue("custom_price")

	_, err := h.DB.Exec(`
		INSERT INTO portfolio (user_id, card_id, notes, custom_price, condition, format)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, cardID, notes, customPrice, "Near Mint", "Raw")
	
	if err != nil {
		slog.Error("Failed to add card to portfolio", "error", err)
		http.Error(w, "Failed to add card", http.StatusInternalServerError)
		return
	}

	// Award XP
	_, _, _ = h.Game.AddXP(userID, 100)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Card added to collection! (+100 XP)"))
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Heartbeat", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Award 1 XP for being active
	newXP, newRank, err := h.Game.AddXP(userID, 1)
	if err != nil {
		slog.Error("Failed to award heartbeat XP", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"xp":   newXP,
		"rank": newRank,
	})
}

func (h *Handler) EditPortfolioItem(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: EditPortfolioItem", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(auth.UserContextKey{}).(string)
	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}
	notes := r.FormValue("notes")
	grade := r.FormValue("grade")
	customPrice := r.FormValue("custom_price")
	isPublic := r.FormValue("is_public") == "true"

	_, err := h.DB.Exec(`
		UPDATE portfolio 
		SET notes = $1, grade = $2, custom_price = $3, is_public = $4
		WHERE id = $5 AND user_id = $6`,
		notes, grade, customPrice, isPublic, itemID, userID)
	if err != nil {
		slog.Error("Failed to edit portfolio item", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Audit the change
	metadata := map[string]interface{}{
		"item_id":      itemID,
		"notes":        notes,
		"grade":        grade,
		"custom_price": customPrice,
		"is_public":    isPublic,
	}
	h.Audit.Log(userID, "edit_portfolio_item", metadata)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Item updated successfully!"))
}


func (h *Handler) AutoNameBinder(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: AutoNameBinder", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := r.Context().Value(auth.UserContextKey{}).(string)
	binderID := r.FormValue("binder_id")

	// Fetch cards in binder
	rows, err := h.DB.Query(`
		SELECT c.name
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.binder_id = $1 AND p.user_id = $2`, binderID, userID)
	
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cards []models.Card
	for rows.Next() {
		var c models.Card
		if err := rows.Scan(&c.Name); err == nil {
			cards = append(cards, c)
		}
	}

	llm := service.NewLLMService()
	newName, err := llm.GenerateBinderName(cards)
	if err != nil {
		slog.Error("LLM: Failed to generate binder name", "error", err)
		http.Error(w, "AI generation failed", http.StatusInternalServerError)
		return
	}

	_, err = h.DB.Exec("UPDATE binders SET name = $1 WHERE id = $2 AND user_id = $3", newName, binderID, userID)
	if err != nil {
		http.Error(w, "Failed to update binder", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": newName})
}

func (h *Handler) Centering(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Centering", "method", r.Method, "url", r.URL.String())
	h.render(w, r, "centering_tool.html", nil)
}

func (h *Handler) Auth(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Auth", "method", r.Method, "url", r.URL.String())
	templateName := "auth.html"
	if r.Header.Get("HX-Request") == "true" {
		templateName = "auth_fragment.html" // Added .html extension for safety
	}

	h.render(w, r, templateName, nil)
}

func (h *Handler) Binders(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Binders", "method", r.Method, "url", r.URL.String())
	h.render(w, r, "binders.html", nil)
}

func (h *Handler) Trade(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Trade", "method", r.Method, "url", r.URL.String())
	h.render(w, r, "trade.html", nil)
}

func (h *Handler) APIScan(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: APIScan", "method", r.Method, "url", r.URL.String())
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
	var processedImg []byte
	if detectedCard == "" {
		var ocrMatch string
		text, ocrMatch, processedImg, err = service.ProcessCardScan(imgBytes, h.MockCards, lang)
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
	resp := map[string]interface{}{
		"text":     strings.ReplaceAll(text, "\n", " "),
		"detected": detectedCard,
		"id":       detectedID,
	}
	if processedImg != nil {
		resp["processed_image"] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(processedImg)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}
