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

// Limits for collector-supplied fields of a synthetic (uncataloged) card. The
// row lands in the shared cards table, so its strings are bounded well below
// the 1 MB request cap.
const (
	maxSyntheticNameRunes   = 200
	maxSyntheticSetRunes    = 200
	maxSyntheticNumberRunes = 32
	maxSyntheticLangRunes   = 16
	maxSyntheticGameRunes   = 32
	// maxSyntheticCardIDRunes bounds a submitted catalog id that only feeds the
	// synthetic id hash (see AddCardToPortfolio).
	maxSyntheticCardIDRunes = 128
)

// validateSyntheticCardFields reports the first collector-supplied field of a
// synthetic card that exceeds its limit, or "" when all fit.
func validateSyntheticCardFields(card models.Card) string {
	return textTooLong(
		textField{"card_name", card.Name, maxSyntheticNameRunes},
		textField{"set_name", card.Set, maxSyntheticSetRunes},
		textField{"set_code", card.SetCode, maxSyntheticSetRunes},
		textField{"collector_number", card.CollectorNumber, maxSyntheticNumberRunes},
		textField{"language", card.Language, maxSyntheticLangRunes},
		textField{"game", card.Game, maxSyntheticGameRunes},
	)
}

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

// persistSyntheticCard inserts the synthetic card into the cards table so the
// collector can add it to a collection or binder.
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
	// cards.source_id references catalog_sources, and no catalog sync ever
	// creates a "claude" source. Without this row the insert fails silently and
	// the UI then cannot add the card it just offered.
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO catalog_sources (id, game, name, base_url, coverage_note)
		VALUES ('claude', 'multiple', 'Claude detection', 'https://pokget.local/claude',
			'Cards detected by the scan model that are not yet in a synced catalog.')
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		slog.Warn("Failed to ensure synthetic card source", "error", err)
		return
	}
	// Inactive and never renamed: the row only backs the collector's own
	// portfolio entry and stays out of every scan's candidates (ReloadCardsOnly
	// also skips the 'claude' source for rows written before this rule).
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO cards (
			id, name, set_name, set_code, collector_number, language, game, variant,
			source_id, source_card_id, catalog_active, source_metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'claude', $1, false, '{}')
		ON CONFLICT (id) DO NOTHING
	`, card.ID, card.Name, setName, card.SetCode, card.CollectorNumber, card.Language, card.Game, card.Variant)
	if err != nil {
		slog.Warn("Failed to persist synthetic card", "card_id", card.ID, "name", card.Name, "error", err)
	}
}
