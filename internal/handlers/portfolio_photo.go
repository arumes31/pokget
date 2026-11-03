package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"

	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"
)

// scanPhotoTokenBytes makes the staged photo token unguessable.
const scanPhotoTokenBytes = 12

// stageScanPhoto keeps the image the scanner just uploaded so the collector can
// attach it to the saved card. It returns an opaque token, or "" when photos are
// disabled or the image cannot be stored.
func (h *Handler) stageScanPhoto(r *http.Request, image []byte) string {
	if h.Photos == nil || !h.Photos.Enabled() || len(image) == 0 {
		return ""
	}
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		return ""
	}
	buffer := make([]byte, scanPhotoTokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	token, err := h.Photos.Stage(userID, hex.EncodeToString(buffer), image)
	if err != nil {
		slog.Debug("Scan photo: cannot stage the uploaded image", "error", err)
		return ""
	}
	return token
}

// attachScanPhoto moves a staged scan photo onto the saved portfolio item, so
// the collection shows the collector's own photo instead of the catalog image.
func (h *Handler) attachScanPhoto(ctx context.Context, userID, itemID, token string) {
	if token == "" || h.Photos == nil || !h.Photos.Enabled() || h.DB == nil {
		return
	}
	relative, err := h.Photos.Promote(userID, token, itemID)
	if err != nil {
		slog.Warn("Portfolio photo: cannot store the scan photo", "item_id", itemID, "error", err)
		return
	}
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE portfolio SET photo_path = $1, photo_updated_at = NOW()
		WHERE id = $2 AND user_id = $3`, relative, itemID, userID); err != nil {
		slog.Warn("Portfolio photo: cannot link the scan photo", "item_id", itemID, "error", err)
		_ = h.Photos.Remove(relative)
	}
}

// PortfolioPhoto serves the photo a collector took of their own card. The
// ownership check is the portfolio row itself, so photo paths never leak.
func (h *Handler) PortfolioPhoto(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if h.DB == nil || h.Photos == nil || !h.Photos.Enabled() {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}
	itemID := mux.Vars(r)["id"]
	var relative string
	var updatedAt sql.NullTime
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(photo_path, ''), photo_updated_at
		FROM portfolio WHERE id = $1 AND user_id = $2`, itemID, userID).
		Scan(&relative, &updatedAt); err != nil {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(relative) == "" {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}

	etag := ""
	if updatedAt.Valid {
		etag = fmt.Sprintf(`"%d"`, updatedAt.Time.UTC().Unix())
		w.Header().Set("ETag", etag)
		if etag != "" && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	data, err := h.Photos.Read(relative)
	if err != nil {
		slog.Warn("Portfolio photo: cannot read stored photo", "item_id", itemID, "error", err)
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", photoContentType(relative))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		slog.Debug("Portfolio photo: write failed", "item_id", itemID, "error", err)
	}
}

func photoContentType(relative string) string {
	switch strings.ToLower(filepath.Ext(relative)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// PortfolioCard renders one collection card: the collector's photo, the printing
// details and both market prices. The collection grid links here.
func (h *Handler) PortfolioCard(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	itemID := mux.Vars(r)["id"]

	var (
		item       models.PortfolioItem
		photo      string
		usd, eur   decimal.NullDecimal
		eurViaRate bool
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT p.id, p.condition, COALESCE(p.grade, ''), COALESCE(p.notes, ''),
		       p.custom_price, p.is_public, COALESCE(p.photo_path, ''), COALESCE(p.binder_id::text, ''),
		       c.id, c.name, c.set_name, COALESCE(c.set_code, ''), COALESCE(c.collector_number, ''),
		       COALESCE(c.language, ''), COALESCE(c.game, ''), COALESCE(c.rarity, ''),
		       COALESCE(c.image_url, ''), c.price_usd, c.price_eur, COALESCE(c.price_eur_estimated, FALSE),
		       COALESCE(c.price_variant, '')
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.id = $1 AND p.user_id = $2`, itemID, userID).
		Scan(&item.ID, &item.Condition, &item.Grade, &item.Notes, &item.CustomPrice, &item.IsPublic,
			&photo, &item.BinderID, &item.Card.ID, &item.Card.Name, &item.Card.Set, &item.Card.SetCode,
			&item.Card.CollectorNumber, &item.Card.Language, &item.Card.Game, &item.Card.Rarity,
			&item.Card.ImageURL, &usd, &eur, &eurViaRate, &item.Card.PriceVariant)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Card not found", http.StatusNotFound)
			return
		}
		slog.Error("Portfolio card: query failed", "item_id", itemID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	setCardMarketPrices(&item.Card, usd, eur)

	photoURL := ""
	if strings.TrimSpace(photo) != "" {
		photoURL = "/portfolio/" + item.ID + "/photo"
	}
	h.render(w, r, "card_detail.html", map[string]interface{}{
		"Item":      item,
		"PhotoURL":  photoURL,
		"HasPhoto":  photoURL != "",
		"MarketUSD": item.Card.PriceUSDValid,
		"MarketEUR": item.Card.PriceEURValid,
		// EUREstimated marks a price converted from USD with the daily ECB rate.
		"EUREstimated": eurViaRate,
		// NoMarketSource tells a game without any price source (Altered, Duel
		// Masters) apart from a card a source simply has no price for.
		"NoMarketSource": !service.HasMarketPriceSource(item.Card.Game),
		"UpdatedAt":      time.Now().UTC(),
	})
}
