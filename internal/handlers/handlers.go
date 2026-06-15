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
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

type Binder struct {
	ID          string
	Name        string
	Description string
	CardCount   int
	UpdatedAt   string
}

type Handler struct {
	Templates     *template.Template
	MockCards     []models.Card
	CardsMu       sync.RWMutex // Protects concurrent access to MockCards
	Fingerprint   *service.FingerprintService
	Detection     *service.DetectionPipeline // SCAN-07, SCAN-09, SCAN-16: Detection pipeline
	Mailer        service.Mailer
	Audit         *service.AuditService
	Crypto        *service.CryptoService
	Game          *service.GamificationService
	LLM           *service.LLMService
	DB            *sql.DB
	BuildVersion  string
	SecureCookies bool // BUG-C03: Configurable Secure flag for session cookies
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["CSRFToken"] = csrf.Token(r)
	data["BuildVersion"] = h.BuildVersion

	// BUG-C04 FIX: Check if DB is nil before querying to prevent nil pointer dereference
	// when the database connection fails at startup.
	if h.DB == nil {
		slog.Error("Database connection is nil, cannot render page")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Inject Global User Data (XP, Rank)
	if userID, ok := r.Context().Value(auth.UserContextKey{}).(string); ok {
		var xp int
		var rankTitle string
		var currency string
		_ = h.DB.QueryRow("SELECT xp, rank_title, currency FROM users WHERE id = $1", userID).Scan(&xp, &rankTitle, &currency)

		if currency == "" {
			currency = "EUR"
		}

		rank := h.Game.GetUserRank(xp)
		_, _, xpPercent := h.Game.GetProgressToNextRank(xp)

		data["UserXP"] = xp
		data["UserRank"] = rankTitle
		data["UserXPPercent"] = xpPercent
		data["UserRankIcon"] = rank.IconURL
		data["UserCurrency"] = currency
		data["CurrencySymbol"] = "€"
		if currency == "USD" {
			data["CurrencySymbol"] = "$"
		}
	}

	if err := h.Templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("Template execution failed", "template", name, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Index", "method", r.Method, "url", r.URL.String())
	session, _ := auth.Store.Get(r, "session")
	userID, ok := session.Values["user_id"].(string)
	if !ok || userID == "" {
		http.Redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	// BUG-M02 FIX: Query the user's actual portfolio instead of using MockCards.
	// Previously, the index page displayed mock/seed cards instead of the
	// authenticated user's real portfolio data.
	var userCurrency string
	_ = h.DB.QueryRow("SELECT currency FROM users WHERE id = $1", userID).Scan(&userCurrency)
	if userCurrency == "" {
		userCurrency = "EUR"
	}

	rows, err := h.DB.Query(`
		SELECT p.id, p.condition, p.custom_price, c.id, c.name, c.set_name, c.image_url, c.price_usd, c.price_eur, c.game
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1`, userID)

	portfolio := make([]models.PortfolioItem, 0, 64)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p models.PortfolioItem
			if err := rows.Scan(&p.ID, &p.Condition, &p.CustomPrice, &p.Card.ID, &p.Card.Name, &p.Card.Set, &p.Card.ImageURL, &p.Card.PriceUSD, &p.Card.PriceEUR, &p.Card.Game); err == nil {
				portfolio = append(portfolio, p)
			}
		}
	}

	h.render(w, r, "index.html", map[string]interface{}{
		"Portfolio": portfolio,
		"Currency":  userCurrency,
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Dashboard", "method", r.Method, "url", r.URL.String())
	// BUG-M04 FIX: Read currency from user's settings instead of URL query parameter.
	// Previously, the dashboard always defaulted to USD regardless of the user's
	// currency preference, causing prices to display in the wrong currency.
	var currency string
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if ok {
		_ = h.DB.QueryRow("SELECT currency FROM users WHERE id = $1", userID).Scan(&currency)
	}
	if currency == "" {
		currency = "EUR"
	}

	// userID already extracted above for currency lookup
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

	// BUG-H03 FIX: Added p.user_id = $1 filter to the LEFT JOIN condition
	// so that owned_cards only counts the current user's portfolio items.
	// Previously, the JOIN matched all users' portfolio items, returning
	// combined completion across ALL users.
	rows, err := h.DB.Query(`
		SELECT
			c.set_name,
			COUNT(DISTINCT c.id) FILTER (WHERE p.id IS NOT NULL) as owned_cards,
			COUNT(DISTINCT c.id) as total_cards
		FROM cards c
		LEFT JOIN portfolio p ON c.id = p.card_id AND p.user_id = $1
		GROUP BY c.set_name`, userID)

	setCompletion := make([]SetProgress, 0, 8) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
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
		SELECT p.id, p.condition, p.custom_price, c.id, c.name, c.set_name, c.image_url, c.price_usd, c.price_eur, c.game
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1`, userID)

	portfolio := make([]models.PortfolioItem, 0, 64) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
	if rowsPortfolio != nil {
		defer rowsPortfolio.Close()
		for rowsPortfolio.Next() {
			var p models.PortfolioItem
			if err := rowsPortfolio.Scan(&p.ID, &p.Condition, &p.CustomPrice, &p.Card.ID, &p.Card.Name, &p.Card.Set, &p.Card.ImageURL, &p.Card.PriceUSD, &p.Card.PriceEUR, &p.Card.Game); err == nil {
				portfolio = append(portfolio, p)
			}
		}
	}

	// Calculate Total Valuation with multipliers
	var totalValuation float64
	var multipliers map[string]float64
	var multStr string
	var userCurrency string
	_ = h.DB.QueryRow("SELECT condition_multipliers, currency FROM users WHERE id = $1", userID).Scan(&multStr, &userCurrency)
	_ = json.Unmarshal([]byte(multStr), &multipliers)

	if userCurrency == "" {
		userCurrency = "EUR"
	}

	priceService := &service.ScraperPriceClient{}
	for _, item := range portfolio {
		if item.CustomPrice != nil {
			totalValuation += *item.CustomPrice
		} else {
			var price float64
			if userCurrency == "EUR" {
				price, _ = item.Card.PriceEUR.Float64()
			} else {
				price, _ = item.Card.PriceUSD.Float64()
			}
			totalValuation += priceService.ApplyMultiplier(price, item.Condition, multipliers)
		}
	}

	// Fetch User XP and Rank
	var xp int
	var rankTitle string
	_ = h.DB.QueryRow("SELECT xp, rank_title FROM users WHERE id = $1", userID).Scan(&xp, &rankTitle)

	rank := h.Game.GetUserRank(xp)
	_, _, xpPercent := h.Game.GetProgressToNextRank(xp)

	// Fetch Binder Count
	var binderCount int
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM binders WHERE user_id = $1", userID).Scan(&binderCount)

	// Fetch 24h Change
	var change24h float64
	var oldValuation float64
	err = h.DB.QueryRow(`
		SELECT valuation 
		FROM portfolio_history 
		WHERE user_id = $1 AND recorded_at <= NOW() - INTERVAL '24 hours'
		ORDER BY recorded_at DESC LIMIT 1`, userID).Scan(&oldValuation)
	if err == nil && oldValuation > 0 {
		change24h = ((totalValuation - oldValuation) / oldValuation) * 100
	}

	h.render(w, r, "dashboard.html", map[string]interface{}{
		"Currency":       currency,
		"TotalValuation": totalValuation,
		"Change24h":      change24h,
		"BinderCount":    binderCount,
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

	// BUG-H01 FIX: Validate required fields before inserting.
	cardID := r.FormValue("card_id")
	if cardID == "" {
		http.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}

	// Verify the card actually exists in the database
	var exists bool
	err := h.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM cards WHERE id = $1)", cardID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Invalid card_id: card not found", http.StatusBadRequest)
		return
	}
	notes := r.FormValue("notes")
	customPriceStr := r.FormValue("custom_price")
	var customPrice *float64
	if customPriceStr != "" {
		val, err := strconv.ParseFloat(customPriceStr, 64)
		if err != nil {
			http.Error(w, "Invalid custom price", http.StatusBadRequest)
			return
		}
		customPrice = &val
	}
	binderID := r.FormValue("binder_id")

	// If binderID is empty, try to find the default binder
	if binderID == "" {
		err := h.DB.QueryRow("SELECT id FROM binders WHERE user_id = $1 AND is_default = TRUE", userID).Scan(&binderID)
		if err != nil {
			slog.Warn("No default binder found for user, using NULL", "user_id", userID)
			binderID = "" // This will result in a NULL binder_id in the DB
		}
	}

	_, err = h.DB.Exec(`
		INSERT INTO portfolio (user_id, card_id, binder_id, notes, custom_price, condition, format)
		VALUES ($1, $2, NULLIF($3, '')::UUID, $4, $5, $6, $7)`,
		userID, cardID, binderID, notes, customPrice, "Near Mint", "Raw")

	if err != nil {
		slog.Error("Failed to add card to portfolio", "error", err)
		http.Error(w, "Failed to add card", http.StatusInternalServerError)
		return
	}

	// Award XP
	_, _, _ = h.Game.AddXP(userID, 100)

	w.Header().Set("HX-Trigger", `{"notify": {"msg": "Asset Secured: Card added to Vault (+100 XP)", "type": "success"}}`)
	w.WriteHeader(http.StatusOK)
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

	// BUG-H05 FIX: Properly check authentication and verify ownership.
	// Previously, userID was extracted with a type assertion that silently
	// ignored failure (using `_, _`), allowing unauthenticated or wrong-user
	// edits to proceed.
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}

	// Verify the portfolio item belongs to the current user
	var ownerID string
	err := h.DB.QueryRow("SELECT user_id FROM portfolio WHERE id = $1", itemID).Scan(&ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Portfolio item not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to verify portfolio item ownership", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if ownerID != userID {
		http.Error(w, "Forbidden: you do not own this portfolio item", http.StatusForbidden)
		return
	}

	notes := r.FormValue("notes")
	grade := r.FormValue("grade")
	customPriceStr := r.FormValue("custom_price")
	var customPrice *float64
	if customPriceStr != "" {
		val, err := strconv.ParseFloat(customPriceStr, 64)
		if err != nil {
			http.Error(w, "Invalid custom price", http.StatusBadRequest)
			return
		}
		customPrice = &val
	}
	isPublic := r.FormValue("is_public") == "true"

	_, err = h.DB.Exec(`
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

	// BUG-M09 FIX: Set Content-Type header for API responses.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Item updated successfully!"))
}

// BUG-H02 FIX: DeletePortfolioItem handler with ownership verification.
// Any authenticated user could previously delete any portfolio item by guessing the ID.
// Now we verify the portfolio item belongs to the current user before deleting.
func (h *Handler) DeletePortfolioItem(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: DeletePortfolioItem", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}

	// Verify the portfolio item belongs to the current user
	var ownerID string
	err := h.DB.QueryRow("SELECT user_id FROM portfolio WHERE id = $1", itemID).Scan(&ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Portfolio item not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to verify portfolio item ownership", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if ownerID != userID {
		http.Error(w, "Forbidden: you do not own this portfolio item", http.StatusForbidden)
		return
	}

	_, err = h.DB.Exec("DELETE FROM portfolio WHERE id = $1 AND user_id = $2", itemID, userID)
	if err != nil {
		slog.Error("Failed to delete portfolio item", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	h.Audit.Log(userID, "delete_portfolio_item", map[string]interface{}{"item_id": itemID})

	// BUG-M09 FIX: Set Content-Type header for API responses.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Item deleted successfully!"))
}

func (h *Handler) AutoNameBinder(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: AutoNameBinder", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// BUG-H06 FIX: Properly check authentication and verify binder ownership.
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	binderID := r.FormValue("binder_id")
	if binderID == "" {
		http.Error(w, "binder_id is required", http.StatusBadRequest)
		return
	}

	// Verify the binder belongs to the current user
	var ownerID string
	err := h.DB.QueryRow("SELECT user_id FROM binders WHERE id = $1", binderID).Scan(&ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Binder not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to verify binder ownership", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if ownerID != userID {
		http.Error(w, "Forbidden: you do not own this binder", http.StatusForbidden)
		return
	}

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

	cards := make([]models.Card, 0, 32) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
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

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := h.DB.Query(`
		SELECT b.id, b.name, b.description, b.created_at, COUNT(p.id) as card_count
		FROM binders b
		LEFT JOIN portfolio p ON b.id = p.binder_id
		WHERE b.user_id = $1
		GROUP BY b.id, b.name, b.description, b.created_at
		ORDER BY b.created_at DESC`, userID)

	binders := make([]Binder, 0, 8) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var b Binder
			var createdAt string
			if err := rows.Scan(&b.ID, &b.Name, &b.Description, &createdAt, &b.CardCount); err == nil {
				b.UpdatedAt = createdAt // Simple assignment for now
				binders = append(binders, b)
			}
		}
	}

	h.render(w, r, "binders.html", map[string]interface{}{
		"Binders": binders,
	})
}

func (h *Handler) CreateBinder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	name := r.FormValue("name")
	description := r.FormValue("description")
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	_, err := h.DB.Exec("INSERT INTO binders (user_id, name, description) VALUES ($1, $2, $3)", userID, name, description)
	if err != nil {
		slog.Error("Failed to create binder", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	h.Binders(w, r)
}

func (h *Handler) BinderDetail(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: BinderDetail", "method", r.Method, "url", r.URL.String())

	vars := mux.Vars(r)
	binderID := vars["id"]

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch binder info
	var binder Binder
	err := h.DB.QueryRow("SELECT id, name, description FROM binders WHERE id = $1 AND user_id = $2", binderID, userID).Scan(&binder.ID, &binder.Name, &binder.Description)
	if err != nil {
		http.Error(w, "Binder not found", http.StatusNotFound)
		return
	}

	// Fetch cards in binder
	rows, err := h.DB.Query(`
		SELECT p.id, p.condition, p.custom_price, c.id, c.name, c.set_name, c.image_url, c.price_usd, c.price_eur, c.game
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.binder_id = $1 AND p.user_id = $2`, binderID, userID)

	cards := make([]models.PortfolioItem, 0, 64) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p models.PortfolioItem
			if err := rows.Scan(&p.ID, &p.Condition, &p.CustomPrice, &p.Card.ID, &p.Card.Name, &p.Card.Set, &p.Card.ImageURL, &p.Card.PriceUSD, &p.Card.PriceEUR, &p.Card.Game); err == nil {
				cards = append(cards, p)
			}
		}
	}

	h.render(w, r, "binder_detail.html", map[string]interface{}{
		"Binder": binder,
		"Cards":  cards,
	})
}

func (h *Handler) Trade(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Trade", "method", r.Method, "url", r.URL.String())
	h.render(w, r, "trade.html", nil)
}

func (h *Handler) RefreshCache(w http.ResponseWriter, r *http.Request) {
	slog.Info("Action: RefreshCache", "user", r.Context().Value(auth.UserContextKey{}))

	count, err := h.reloadCards()
	if err != nil {
		http.Error(w, "Failed to refresh cache: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// BUG-M09 FIX: Set Content-Type header for API responses.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("Successfully reloaded %d cards", count)))
}

func (h *Handler) reloadCards() (int, error) {
	rows, err := h.DB.Query("SELECT id, name, set_name, price_usd, price_eur, image_url, variant, change_24h, phash FROM cards")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	allCards := make([]models.Card, 0, 1024) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations for cache reload
	for rows.Next() {
		var c models.Card
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &c.Phash); err != nil {
			continue
		}
		allCards = append(allCards, c)
	}

	h.CardsMu.Lock()
	h.MockCards = allCards
	h.CardsMu.Unlock()
	slog.Info("Database: Reloaded cards into cache", "count", len(allCards))
	return len(allCards), nil
}

func (h *Handler) APIScan(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: APIScan", "method", r.Method, "url", r.URL.String())

	// Snapshot MockCards under read lock to avoid races with reloadCards
	h.CardsMu.RLock()
	cards := make([]models.Card, len(h.MockCards))
	copy(cards, h.MockCards)
	h.CardsMu.RUnlock()

	// Limit request body to 10MB to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	err := r.ParseMultipartForm(10 << 20) // #nosec G120 - bounded by MaxBytesReader
	if err != nil {
		http.Error(w, "Failed to parse form or file too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("card_image")
	if err != nil {
		slog.Warn("APIScan: Failed to get image from form", "error", err)
		http.Error(w, "Failed to get image from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	slog.Info("APIScan: Received image", "filename", header.Filename, "size", header.Size)

	lang := r.FormValue("lang")

	imgBytes, err := io.ReadAll(file)
	if err != nil {
		slog.Error("APIScan: Failed to read image", "error", err)
		http.Error(w, "Failed to read image", http.StatusInternalServerError)
		return
	}

	if len(imgBytes) == 0 {
		slog.Warn("APIScan: Received empty image bytes")
		http.Error(w, "Empty image received", http.StatusBadRequest)
		return
	}

	// Get user currency preference
	var userCurrency string
	if userID, ok := r.Context().Value(auth.UserContextKey{}).(string); ok {
		_ = h.DB.QueryRow("SELECT currency FROM users WHERE id = $1", userID).Scan(&userCurrency)
	}
	if userCurrency == "" {
		userCurrency = "EUR"
	}

	// Create a context with timeout for OCR
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Check context before starting
	if ctx.Err() != nil {
		http.Error(w, "Request timed out", http.StatusRequestTimeout)
		return
	}

	var detectedCard string
	var detectedID string
	var text string
	var detectedPrice float64
	var detectedImage string
	var processedImg []byte

	// SCAN-07, SCAN-09, SCAN-16: Use detection pipeline if available
	if h.Detection != nil {
		result := h.Detection.Detect(imgBytes, cards, lang)
		text = result.OCRText
		processedImg = result.ProcessedImage

		if best := result.BestMatchCard(); best != nil {
			detectedCard = best.Name
			detectedID = best.ID
			if userCurrency == "EUR" {
				detectedPrice, _ = best.PriceEUR.Float64()
			} else {
				detectedPrice, _ = best.PriceUSD.Float64()
			}
			detectedImage = best.ImageURL
		}

		// Build top matches for API response (SCAN-09)
		topMatches := make([]map[string]interface{}, 0, len(result.TopMatches))
		for _, m := range result.TopMatches {
			matchEntry := map[string]interface{}{
				"name":         m.Card.Name,
				"id":           m.Card.ID,
				"confidence":   m.Confidence,
				"needs_review": m.NeedsReview,
			}
			if userCurrency == "EUR" {
				matchEntry["price"], _ = m.Card.PriceEUR.Float64()
			} else {
				matchEntry["price"], _ = m.Card.PriceUSD.Float64()
			}
			matchEntry["image_url"] = m.Card.ImageURL
			topMatches = append(topMatches, matchEntry)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"text":             strings.ReplaceAll(text, "\n", " "),
			"detected":         detectedCard,
			"id":               detectedID,
			"price":            detectedPrice,
			"image_url":        detectedImage,
			"confidence":       result.BestMatchConfidence(),
			"needs_review":     result.BestMatchNeedsReview(),
			"top_matches":      topMatches,
			"pipeline_metrics": result.Metrics.Format(), // SCAN-16
		}
		if processedImg != nil {
			resp["processed_image"] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(processedImg)
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Failed to encode JSON response", "error", err)
		}
		return
	}

	// Fallback: Legacy sequential pipeline (backward-compatible)
	// 1. Visual Fingerprint Matching (FAST & Language Independent)
	if h.Fingerprint != nil {
		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			slog.Warn("Fingerprint: Failed to decode image", "error", err)
		} else {
			hash, err := h.Fingerprint.CalculateHash(img)
			if err != nil {
				slog.Warn("Fingerprint: Failed to calculate hash", "error", err)
			} else {
				match, distance, _ := h.Fingerprint.MatchFingerprint(hash, cards)
				if match != nil {
					slog.Info("Fingerprint: Found match", "name", match.Name, "distance", distance)
					detectedCard = match.Name
					detectedID = match.ID

					if userCurrency == "EUR" {
						detectedPrice, _ = match.PriceEUR.Float64()
					} else {
						detectedPrice, _ = match.PriceUSD.Float64()
					}
					detectedImage = match.ImageURL
				} else {
					slog.Info("Fingerprint: No match found")
				}
			}
		}
	}

	// 2. OCR Fallback (if visual matching fails)
	if detectedCard == "" {
		slog.Info("APIScan: Fingerprint missed, falling back to OCR")
		var ocrMatch string
		text, ocrMatch, processedImg, err = service.ProcessCardScan(imgBytes, cards, lang, h.LLM)
		if err != nil {
			slog.Error("OCR: Failed to process scan", "error", err)
			http.Error(w, "Detection failed", http.StatusInternalServerError)
			return
		}
		if ocrMatch != "Unknown Card" {
			slog.Info("OCR Fallback: Found match", "name", ocrMatch)
			detectedCard = ocrMatch
			for _, c := range cards {
				if c.ID == ocrMatch || c.Name == ocrMatch {
					detectedID = c.ID
					if userCurrency == "EUR" {
						detectedPrice, _ = c.PriceEUR.Float64()
					} else {
						detectedPrice, _ = c.PriceUSD.Float64()
					}
					detectedImage = c.ImageURL
					detectedCard = c.Name // Ensure detectedCard is the name
					break
				}
			}
		} else {
			slog.Info("OCR Fallback: No match found")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"text":      strings.ReplaceAll(text, "\n", " "),
		"detected":  detectedCard,
		"id":        detectedID,
		"price":     detectedPrice,
		"image_url": detectedImage,
	}
	if processedImg != nil {
		resp["processed_image"] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(processedImg)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}
