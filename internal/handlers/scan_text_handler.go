package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"
)

const maxTextScanRequestBytes int64 = 16 << 10

// executeTextScan uses the same authenticated, CSRF-protected, rate-limited
// /api/scan route as image uploads. Only observations cross this boundary.
func (h *Handler) executeTextScan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTextScanRequestBytes)
	var input struct {
		Text     string `json:"ocr_text"`
		Game     string `json:"game"`
		Language string `json:"lang"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	if err == nil {
		err = decoder.Decode(new(any))
		if errors.Is(err, io.EOF) {
			err = nil
		} else if err == nil {
			err = service.ErrInvalidDetectionRequest
		}
	}
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			http.Error(w, "Card image is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid scan request", http.StatusBadRequest)
		}
		return
	}
	tcg, gameErr := models.ParseTCG(input.Game)
	language, langErr := models.ParseLanguage(input.Language)
	if gameErr != nil || langErr != nil || input.Game == "" || input.Language == "" {
		http.Error(w, "Invalid scan request", http.StatusBadRequest)
		return
	}
	h.CardsMu.RLock()
	cards := append([]models.Card(nil), h.MockCards...)
	h.CardsMu.RUnlock()
	budget := 10 * time.Second
	if h.ScanTimeout > 0 {
		budget = min(budget, h.ScanTimeout)
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	select {
	case scanDetectionSlots <- struct{}{}:
		defer func() { <-scanDetectionSlots }()
	case <-ctx.Done():
		writeDetectionError(w, ctx.Err())
		return
	}
	pipeline := h.Detection
	if pipeline == nil {
		pipeline = service.NewDetectionPipeline(nil, h.LLM)
	}
	result, err := pipeline.DetectTextScoped(ctx, service.TextDetectionRequest{
		Text: input.Text, Cards: cards, Scope: service.ScanScope{TCG: tcg, Language: language},
	})
	if err != nil {
		writeDetectionError(w, err)
		return
	}
	currency := "EUR"
	if userID, ok := r.Context().Value(auth.UserContextKey{}).(string); ok && h.DB != nil {
		_ = h.DB.QueryRowContext(r.Context(), "SELECT currency FROM users WHERE id = $1", userID).Scan(&currency)
	}
	matches := make([]map[string]any, 0, len(result.TopMatches))
	for _, match := range result.TopMatches {
		card := match.Card
		price, _ := card.PriceEUR.Float64()
		if currency == "USD" {
			price, _ = card.PriceUSD.Float64()
		}
		matches = append(matches, map[string]any{
			"id": card.ID, "name": card.Name, "set": card.Set, "collector_number": card.CollectorNumber,
			"language": card.Language, "image_url": card.ImageURL, "price": price,
			"confidence": match.Confidence, "needs_review": true,
		})
	}
	response := map[string]any{
		"text": result.OCRText, "id": "", "detected": "", "confidence": result.BestMatchConfidence(),
		"needs_review": true, "top_matches": matches, "processing": "device_ocr",
		"requires_image": len(matches) == 0,
	}
	if len(matches) > 0 {
		for key, value := range matches[0] {
			response[key] = value
		}
		response["detected"] = result.BestMatchName()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}
