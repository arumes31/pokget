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
	"unicode/utf8"

	"pokget/internal/catalog"
	"pokget/internal/models"
)

// Limits for collector-supplied fields of a synthetic (uncataloged) card. The
// row lands in the shared cards table and in the process-wide card cache every
// scan reads, so its strings are bounded well below the 1 MB request cap.
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

// maxSyntheticCacheAppends caps how many synthetic cards persistSyntheticCard
// adds to the in-memory cache between two reloads. Past the cap the card is
// still persisted (and picked up by the next reload); only the cache append is
// skipped, so a collector cannot grow shared memory without bound.
const maxSyntheticCacheAppends = 5000

// validateSyntheticCardFields reports the first collector-supplied field of a
// synthetic card that exceeds its limit, or "" when all fit.
func validateSyntheticCardFields(card models.Card) string {
	for _, f := range []struct {
		name, value string
		limit       int
	}{
		{"card_name", card.Name, maxSyntheticNameRunes},
		{"set_name", card.Set, maxSyntheticSetRunes},
		{"set_code", card.SetCode, maxSyntheticSetRunes},
		{"collector_number", card.CollectorNumber, maxSyntheticNumberRunes},
		{"language", card.Language, maxSyntheticLangRunes},
		{"game", card.Game, maxSyntheticGameRunes},
	} {
		if utf8.RuneCountInString(f.value) > f.limit {
			return fmt.Sprintf("%s must be at most %d characters", f.name, f.limit)
		}
	}
	return ""
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
	cardCopy := card
	cardCopy.Set = setName
	cardCopy.CatalogActive = &active
	h.CardsMu.Lock()
	defer h.CardsMu.Unlock()
	// Append each synthetic id at most once per cache generation. The slice is
	// append-only between reloads (executeScan reads its header without
	// copying), so an existing entry is never rewritten in place; a renamed card
	// shows its new name after the next reload.
	if _, seen := h.syntheticCached[card.ID]; seen {
		return
	}
	if len(h.syntheticCached) >= maxSyntheticCacheAppends {
		slog.Warn("Synthetic card cache budget exhausted; card persisted but not cached until next reload",
			"card_id", card.ID, "limit", maxSyntheticCacheAppends)
		return
	}
	if h.syntheticCached == nil {
		h.syntheticCached = make(map[string]struct{})
	}
	h.syntheticCached[card.ID] = struct{}{}
	h.MockCards = append(h.MockCards, cardCopy)
}
