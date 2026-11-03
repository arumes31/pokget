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

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"pokget/internal/models"
)

// DetectionPipeline runs server-side card recognition through the configured
// vision detector. Fingerprint and text-model helpers stay available for other
// features, but no scan path falls back to local OCR.
type DetectionPipeline struct {
	Fingerprint *FingerprintService
	LLM         *LLMService
	Claude      ClaudeDetector
}

// NewDetectionPipeline builds the scan pipeline. The detector is installed by
// the caller once configuration is known.
func NewDetectionPipeline(fingerprint *FingerprintService, llm *LLMService) *DetectionPipeline {
	return &DetectionPipeline{Fingerprint: fingerprint, LLM: llm}
}

// Detect retains the legacy untyped entry point. New callers should use
// DetectScoped so TCG and language are explicit user selections.
func (p *DetectionPipeline) Detect(imgBytes []byte, cards []models.Card, lang string) *DetectionResult {
	return p.DetectContext(context.Background(), imgBytes, cards, lang)
}

// DetectContext infers a scope only when every supplied printing has complete,
// consistent metadata. Otherwise it preserves legacy behavior and absorbs the
// separate error return into status/metrics for source compatibility.
func (p *DetectionPipeline) DetectContext(ctx context.Context, imgBytes []byte, cards []models.Card, lang string) *DetectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return &DetectionResult{
			Status:  DetectionStatusCanceled,
			Metrics: DetectionMetrics{Stages: []DetectionStageMetrics{{Name: "context", Error: err}}},
		}
	}
	if scope, ok := inferScanScope(cards, lang); ok {
		result, err := p.DetectScoped(ctx, DetectionRequest{Image: imgBytes, Cards: cards, Scope: scope})
		if result != nil {
			return result
		}
		return failedDetectionResult(err)
	}

	result, err := p.detect(ctx, DetectionRequest{Image: imgBytes, Cards: activeCards(cards)}, false)
	if result == nil {
		return failedDetectionResult(err)
	}
	return result
}

// DetectScoped validates and enforces the user-selected TCG and language for
// every local matching stage. Operational errors are returned separately from
// the machine-readable result status.
func (p *DetectionPipeline) DetectScoped(ctx context.Context, request DetectionRequest) (*DetectionResult, error) {
	started := time.Now()
	if ctx == nil {
		return invalidDetectionResult(started), fmt.Errorf("%w: nil context", ErrInvalidDetectionRequest)
	}
	if err := ctx.Err(); err != nil {
		result := &DetectionResult{Status: DetectionStatusCanceled}
		result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "context", Error: err})
		result.Metrics.TotalTime = time.Since(started)
		return result, err
	}
	if len(request.Image) == 0 {
		return invalidDetectionResult(started), fmt.Errorf("%w: image is empty", ErrInvalidDetectionRequest)
	}
	if !request.Scope.TCG.Valid() {
		return invalidDetectionResult(started), fmt.Errorf("%w: unsupported TCG %q", ErrInvalidDetectionRequest, request.Scope.TCG)
	}
	if !request.Scope.Language.Valid() {
		return invalidDetectionResult(started), fmt.Errorf("%w: unsupported language %q", ErrInvalidDetectionRequest, request.Scope.Language)
	}
	cachedCards := len(request.Cards)
	request.Cards = cardsForScope(request.Cards, request.Scope)
	ScanLogger(ctx).Info("Scan scope selected", "game", request.Scope.TCG, "language", request.Scope.Language, "cached_cards", cachedCards, "eligible_cards", len(request.Cards))
	if len(request.Cards) == 0 {
		return invalidDetectionResult(started), errors.Join(ErrInvalidDetectionRequest, ErrNoEligibleCards)
	}
	return p.detect(ctx, request, true)
}

func invalidDetectionResult(started time.Time) *DetectionResult {
	return &DetectionResult{Status: DetectionStatusInvalidRequest, Metrics: DetectionMetrics{TotalTime: time.Since(started)}}
}

func failedDetectionResult(err error) *DetectionResult {
	result := &DetectionResult{Status: DetectionStatusFailed}
	if err != nil {
		result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "pipeline", Error: err})
	}
	return result
}

func inferScanScope(cards []models.Card, lang string) (ScanScope, bool) {
	language, err := models.ParseLanguage(lang)
	if err != nil || len(cards) == 0 {
		return ScanScope{}, false
	}
	var selected models.TCG
	for index := range cards {
		card := cards[index]
		if card.ID == "" || !card.IsCatalogActive() || !language.Matches(card.Language) {
			return ScanScope{}, false
		}
		tcg, err := models.ParseTCG(card.Game)
		if err != nil {
			return ScanScope{}, false
		}
		if selected == models.TCGUnknown {
			selected = tcg
		} else if tcg != selected {
			return ScanScope{}, false
		}
	}
	return ScanScope{TCG: selected, Language: language}, selected.Valid()
}

func cardsForScope(cards []models.Card, scope ScanScope) []models.Card {
	byID := make(map[string]models.Card, len(cards))
	for index := range cards {
		card := cards[index]
		if card.ID == "" || !card.IsCatalogActive() || tcgForCard(card) != scope.TCG || !scope.Language.Matches(card.Language) {
			continue
		}
		if existing, exists := byID[card.ID]; !exists || canonicalCardLess(card, existing) {
			byID[card.ID] = card
		}
	}
	filtered := make([]models.Card, 0, len(byID))
	for _, card := range byID {
		filtered = append(filtered, card)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	return filtered
}

func canonicalCardLess(left, right models.Card) bool {
	leftKey := left.SetCode + "\x00" + left.CollectorNumber + "\x00" + left.Set + "\x00" + left.Name + "\x00" + left.Variant
	rightKey := right.SetCode + "\x00" + right.CollectorNumber + "\x00" + right.Set + "\x00" + right.Name + "\x00" + right.Variant
	return leftKey < rightKey
}

func activeCards(cards []models.Card) []models.Card {
	filtered := make([]models.Card, 0, len(cards))
	for index := range cards {
		if cards[index].IsCatalogActive() {
			filtered = append(filtered, cards[index])
		}
	}
	return filtered
}

// detect runs the server-side detector for one upload. Card recognition happens
// exclusively on the server; the caller has already validated the scope.
func (p *DetectionPipeline) detect(ctx context.Context, request DetectionRequest, scoped bool) (*DetectionResult, error) {
	totalStart := time.Now()
	if len(request.Image) == 0 {
		return invalidDetectionResult(totalStart), fmt.Errorf("%w: image is empty", ErrInvalidDetectionRequest)
	}
	if p.Claude == nil {
		// Guessing a card without a detector would put a wrong card in the
		// collection, so an unconfigured server fails loudly instead.
		err := fmt.Errorf("%w: no vision detector is configured", ErrDetectionUnavailable)
		ScanLogger(ctx).Error("Scan rejected: vision detector missing", "error", err)
		return failedDetectionResult(err), err
	}
	result, err := p.detectClaude(ctx, request, scoped, totalStart)
	if result == nil {
		return failedDetectionResult(err), err
	}
	return result, err
}

