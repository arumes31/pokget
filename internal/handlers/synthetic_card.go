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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"pokget/internal/catalog"
	"pokget/internal/models"
)

// generateSyntheticCardID produces a deterministic canonical ID for an uncataloged card
// recognized by multimodal detection.
func generateSyntheticCardID(card models.Card) string {
	game := strings.ToLower(strings.TrimSpace(card.Game))
	if game == "" {
		game = "pokemon"
	}
	set := strings.ToLower(strings.TrimSpace(card.SetCode))
	if set == "" {
		set = strings.ToLower(strings.TrimSpace(card.Set))
	}
	if set == "" {
		set = "unknown"
	}
	num := strings.ToLower(strings.TrimSpace(card.CollectorNumber))
	if num == "" {
		num = "0"
	}
	lang := strings.ToLower(strings.TrimSpace(card.Language))
	if lang == "" {
		lang = "en"
	}
	name := strings.ToLower(strings.TrimSpace(card.Name))

	id, err := catalog.CanonicalID("card", "claude", game, set, num, lang, name)
	if err == nil && id != "" {
		return id
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%s", game, set, num, lang, name)))
	return "card_" + hex.EncodeToString(h[:])
}

// persistSyntheticCard inserts the synthetic card into the database cards table
// and updates the handler's in-memory MockCards cache so it can be added to
// user collections and binders.
func (h *Handler) persistSyntheticCard(ctx context.Context, card models.Card) {
	if h.DB == nil || card.ID == "" || card.Name == "" {
		return
	}
	setName := card.Set
	if setName == "" {
		setName = card.SetCode
	}
	if setName == "" {
		setName = "Uncataloged Set"
	}
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO cards (
			id, name, set_name, set_code, collector_number, language, game, variant,
			source_id, source_card_id, catalog_active, source_metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'claude', $1, true, '{}')
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			set_name = EXCLUDED.set_name,
			catalog_active = true
	`, card.ID, card.Name, setName, card.SetCode, card.CollectorNumber, card.Language, card.Game, card.Variant)
	if err != nil {
		slog.Warn("Failed to persist synthetic card", "card_id", card.ID, "name", card.Name, "error", err)
		return
	}
	active := true
	h.CardsMu.Lock()
	cardCopy := card
	cardCopy.Set = setName
	cardCopy.CatalogActive = &active
	h.MockCards = append(h.MockCards, cardCopy)
	h.CardsMu.Unlock()
}
