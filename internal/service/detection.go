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
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"pokget/internal/models"

	_ "golang.org/x/image/webp"
)

type fingerprintStageRunner func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error)
type ocrStageRunner func(context.Context, []byte, []models.Card, string) (string, string, []byte, error)

// DetectionPipeline runs fingerprint and OCR independently, combines their
// deterministic evidence, and uses an LLM only to choose a shortlisted ID.
type DetectionPipeline struct {
	Fingerprint *FingerprintService
	LLM         *LLMService

	fingerprintRunner fingerprintStageRunner
	ocrRunner         ocrStageRunner
}

// NewDetectionPipeline preserves the legacy constructor while installing
// cancellable stage runners that tests can substitute inside this package.
func NewDetectionPipeline(fingerprint *FingerprintService, llm *LLMService) *DetectionPipeline {
	pipeline := &DetectionPipeline{Fingerprint: fingerprint, LLM: llm}
	pipeline.fingerprintRunner = pipeline.runFingerprintStage
	pipeline.ocrRunner = pipeline.runOCRStage
	return pipeline
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

	result, err := p.detect(ctx, DetectionRequest{Image: imgBytes, Cards: activeCards(cards)}, lang, false)
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
	request.Cards = cardsForScope(request.Cards, request.Scope)
	if len(request.Cards) == 0 {
		return invalidDetectionResult(started), errors.Join(ErrInvalidDetectionRequest, ErrNoEligibleCards)
	}
	return p.detect(ctx, request, request.Scope.Language.TesseractCode(), true)
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

type fingerprintStageOutput struct {
	result   *MatchResult
	duration time.Duration
	err      error
}

type ocrStageOutput struct {
	text           string
	detectedCardID string
	processedImage []byte
	duration       time.Duration
	err            error
}

func (p *DetectionPipeline) detect(ctx context.Context, request DetectionRequest, ocrLanguage string, scoped bool) (*DetectionResult, error) {
	totalStart := time.Now()
	result := &DetectionResult{Status: DetectionStatusUnknown}
	stageCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fingerprintCards := slices.Clone(request.Cards)
	ocrCards := slices.Clone(request.Cards)
	fingerprintChannel := make(chan fingerprintStageOutput, 1)
	ocrChannel := make(chan ocrStageOutput, 1)

	fingerprintRunner := p.fingerprintRunner
	if fingerprintRunner == nil {
		fingerprintRunner = p.runFingerprintStage
	}
	ocrRunner := p.ocrRunner
	if ocrRunner == nil {
		ocrRunner = p.runOCRStage
	}
	var scope *ScanScope
	if scoped {
		scope = &request.Scope
	}
	go func() {
		started := time.Now()
		match, err := fingerprintRunner(stageCtx, request.Image, fingerprintCards, scope)
		fingerprintChannel <- fingerprintStageOutput{result: match, duration: time.Since(started), err: err}
	}()
	go func() {
		started := time.Now()
		text, detected, processed, err := ocrRunner(stageCtx, request.Image, ocrCards, ocrLanguage)
		ocrChannel <- ocrStageOutput{
			text: text, detectedCardID: detected, processedImage: processed,
			duration: time.Since(started), err: err,
		}
	}()

	var fingerprintOutput fingerprintStageOutput
	var ocrOutput ocrStageOutput
	for fingerprintChannel != nil || ocrChannel != nil {
		select {
		case fingerprintOutput = <-fingerprintChannel:
			fingerprintChannel = nil
			if p.applyFingerprintFastPath(result, fingerprintOutput.result) {
				cancel()
				result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{
					Name: "fingerprint", Duration: fingerprintOutput.duration, Error: fingerprintOutput.err,
				})
				result.Metrics.TotalTime = time.Since(totalStart)
				setDetectionStatus(result)
				return result, nil
			}
		case ocrOutput = <-ocrChannel:
			ocrChannel = nil
		case <-ctx.Done():
			cancel()
			result.Status = DetectionStatusCanceled
			result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "context", Error: ctx.Err()})
			result.Metrics.TotalTime = time.Since(totalStart)
			return result, ctx.Err()
		}
	}
	result.OCRText = ocrOutput.text
	result.ProcessedImage = ocrOutput.processedImage
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "fingerprint", Duration: fingerprintOutput.duration, Error: fingerprintOutput.err},
		DetectionStageMetrics{Name: "ocr", Duration: ocrOutput.duration, Error: ocrOutput.err},
	)
	if err := ctx.Err(); err != nil {
		result.Status = DetectionStatusCanceled
		result.Metrics.TotalTime = time.Since(totalStart)
		return result, err
	}

	fingerprintResult := fingerprintOutput.result

	combineStart := time.Now()
	candidateMap := make(map[string]*CardMatch)
	addFingerprintCandidates(candidateMap, fingerprintResult, p.Fingerprint)
	for _, candidate := range resolveOCRCandidates(ocrOutput.detectedCardID, ocrOutput.text, request.Cards) {
		card := cardByID(request.Cards, candidate.Card.ID)
		if card == nil {
			continue
		}
		score := max(ocrScoreFromLevenshtein(ocrOutput.text, card.Name), min(99, float64(50+candidate.Score/25)))
		match := getOrCreateMatch(candidateMap, card)
		match.OCRScore = &ConfidenceScore{
			Method: "ocr", Score: score, CardName: card.Name, CardID: card.ID, RawText: ocrOutput.text,
		}
	}

	for _, match := range candidateMap {
		match.Confidence = combineScores(match.FingerprintScore, match.OCRScore, match.LLMScore)
	}
	if p.LLM != nil && !hasHighConfidenceCandidate(candidateMap, 70) {
		llmCards := candidateCards(candidateMap)
		if len(llmCards) > 0 {
			llmStart := time.Now()
			var llmResponse *LLMCardResponse
			var llmErr error
			if scoped {
				llmResponse, llmErr = p.LLM.FuzzyMatchCardScopedContext(ctx, ocrOutput.text, llmCards, request.Scope)
			} else {
				llmResponse, llmErr = p.LLM.FuzzyMatchCardWithValidationContext(ctx, ocrOutput.text, llmCards)
			}
			result.Metrics.Stages = append(result.Metrics.Stages,
				DetectionStageMetrics{Name: "llm", Duration: time.Since(llmStart), Error: llmErr},
			)
			if llmErr == nil && llmResponse != nil && !llmResponse.Abstained && llmResponse.CardID != "" {
				if match := candidateMap[llmResponse.CardID]; match != nil {
					match.LLMScore = &ConfidenceScore{
						Method: "llm", Score: llmResponse.Confidence * 100,
						CardName: match.Card.Name, CardID: match.Card.ID,
					}
				}
			}
		}
	}

	for _, match := range candidateMap {
		match.Confidence = combineScores(match.FingerprintScore, match.OCRScore, match.LLMScore)
		match.NeedsReview = match.Confidence < 70
	}
	result.TopMatches = sortedTopMatches(candidateMap, 5)
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "combine", Duration: time.Since(combineStart)},
	)
	result.Metrics.TotalTime = time.Since(totalStart)
	setDetectionStatus(result)

	if len(result.TopMatches) == 0 && ocrOutput.err != nil && (p.Fingerprint == nil || fingerprintOutput.err != nil) {
		result.Status = DetectionStatusFailed
		return result, errors.Join(fingerprintOutput.err, ocrOutput.err)
	}
	slog.Info("Detection: Pipeline complete", "status", result.Status, "metrics", result.Metrics.Format(),
		"top_match_id", result.BestMatchID(), "confidence", result.BestMatchConfidence())
	return result, nil
}

func (p *DetectionPipeline) applyFingerprintFastPath(result *DetectionResult, fingerprintResult *MatchResult) bool {
	matches := exactFingerprintMatches(fingerprintResult)
	if len(matches) > 0 {
		setExactFingerprintResult(result, matches)
		return true
	}

	highConfidenceThreshold := DefaultPhashThresholdHighConf
	if p.Fingerprint != nil {
		highConfidenceThreshold = p.Fingerprint.PhashHighConf
	}
	if exact, distance := uniqueHighConfidenceFingerprint(fingerprintResult, highConfidenceThreshold); exact != nil {
		confidence := max(75, 100-float64(distance*5))
		result.TopMatches = []CardMatch{{
			Card: exact,
			FingerprintScore: &ConfidenceScore{
				Method: "fingerprint", Score: confidence, CardName: exact.Name, CardID: exact.ID, Distance: distance,
			},
			Confidence: confidence,
		}}
		return true
	}
	if matches := ambiguousSameNameFingerprints(fingerprintResult, highConfidenceThreshold); len(matches) > 1 {
		for _, match := range matches {
			confidence := 0.5 * fingerprintScoreFromDistance(match.Distance, highConfidenceThreshold)
			result.TopMatches = append(result.TopMatches, CardMatch{
				Card: match.Card,
				FingerprintScore: &ConfidenceScore{
					Method: "fingerprint", Score: confidence * 2, CardName: match.Card.Name,
					CardID: match.Card.ID, Distance: match.Distance,
				},
				Confidence: confidence, NeedsReview: true,
			})
		}
		return true
	}
	return false
}

func (p *DetectionPipeline) runFingerprintStage(ctx context.Context, imageBytes []byte, cards []models.Card, scope *ScanScope) (*MatchResult, error) {
	if p.Fingerprint == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, fmt.Errorf("fingerprint: decode image: %w", err)
	}
	hash, err := p.Fingerprint.CalculateHash(decoded)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: calculate hash: %w", err)
	}
	if scope == nil {
		return p.Fingerprint.SearchByHashWithCards(hash, cards), nil
	}
	return p.Fingerprint.SearchByHashWithScope(hash, FingerprintIndexScope{
		TCG: scope.TCG, Language: scope.Language,
		Algorithm: scope.FingerprintAlgorithm, Version: scope.FingerprintVersion,
	}, cards), nil
}

func (*DetectionPipeline) runOCRStage(ctx context.Context, imageBytes []byte, cards []models.Card, language string) (string, string, []byte, error) {
	// LLM fallback is intentionally disabled here. The pipeline invokes its
	// strict ID-only verifier once, after deterministic candidates exist.
	return ProcessCardScanContext(ctx, imageBytes, cards, language, nil)
}

func setExactFingerprintResult(result *DetectionResult, matches []FingerprintMatch) {
	needsReview := len(matches) > 1
	confidence := 100.0
	if needsReview {
		confidence = 50
	}
	for _, match := range matches {
		result.TopMatches = append(result.TopMatches, CardMatch{
			Card: match.Card,
			FingerprintScore: &ConfidenceScore{
				Method: "fingerprint", Score: 100, CardName: match.Card.Name, CardID: match.Card.ID,
			},
			Confidence: confidence, NeedsReview: needsReview,
		})
	}
}

func addFingerprintCandidates(candidateMap map[string]*CardMatch, result *MatchResult, fingerprint *FingerprintService) {
	if result == nil {
		return
	}
	highThreshold := DefaultPhashThresholdHighConf
	potentialThreshold := DefaultPhashThresholdPotential
	if fingerprint != nil {
		highThreshold = fingerprint.PhashHighConf
		potentialThreshold = fingerprint.PhashPotential
	}
	if result.HighConfidence != nil {
		card := result.HighConfidence
		match := getOrCreateMatch(candidateMap, card)
		if match == nil {
			return
		}
		match.FingerprintScore = &ConfidenceScore{
			Method: "fingerprint", Score: fingerprintScoreFromDistance(result.BestDistance, highThreshold),
			CardName: card.Name, CardID: card.ID, Distance: result.BestDistance,
		}
	}
	for _, potential := range result.Potential {
		if potential.Card == nil {
			continue
		}
		match := getOrCreateMatch(candidateMap, potential.Card)
		if match == nil || match.FingerprintScore != nil {
			continue
		}
		match.FingerprintScore = &ConfidenceScore{
			Method: "fingerprint", Score: fingerprintScoreFromDistance(potential.Distance, potentialThreshold),
			CardName: potential.Card.Name, CardID: potential.Card.ID, Distance: potential.Distance,
		}
	}
}

func resolveOCRCandidates(detected, ocrText string, cards []models.Card) []candidateEvidence {
	detectedNormalized := normalizeMatchText(detected)
	if detectedNormalized != "" && detectedNormalized != normalizeMatchText("Unknown Card") {
		for index := range cards {
			if cards[index].ID == detected {
				evidence := scoreCandidate(normalizeMatchText(ocrText), compactMatchText(ocrText), strings.Fields(normalizeMatchText(ocrText)), cards[index])
				evidence.Score = max(evidence.Score, 1000)
				evidence.Reasons = append(evidence.Reasons, "ocr_card_id")
				return []candidateEvidence{evidence}
			}
		}
		nameMatches := make([]models.Card, 0, 1)
		for index := range cards {
			if normalizeMatchText(cards[index].Name) == detectedNormalized || localizedNameMatches(cards[index], detectedNormalized) {
				nameMatches = append(nameMatches, cards[index])
			}
		}
		if len(nameMatches) > 0 {
			return rankCandidates(ocrText, nameMatches, min(10, len(nameMatches)))
		}
	}

	ranked := rankCandidates(ocrText, cards, min(10, len(cards)))
	matched := ranked[:0]
	for _, candidate := range ranked {
		if candidate.Score >= defaultLLMMinEvidence {
			matched = append(matched, candidate)
		}
	}
	return matched
}

func localizedNameMatches(card models.Card, normalized string) bool {
	for _, name := range card.LocalizedNames {
		if normalizeMatchText(name) == normalized {
			return true
		}
	}
	return false
}

func cardByID(cards []models.Card, id string) *models.Card {
	for index := range cards {
		if cards[index].ID == id {
			return &cards[index]
		}
	}
	return nil
}

func hasHighConfidenceCandidate(candidates map[string]*CardMatch, threshold float64) bool {
	for _, candidate := range candidates {
		if candidate.Confidence >= threshold {
			return true
		}
	}
	return false
}

func candidateCards(candidates map[string]*CardMatch) []models.Card {
	ids := make([]string, 0, len(candidates))
	for id, candidate := range candidates {
		if id != "" && candidate != nil && candidate.Card != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	cards := make([]models.Card, 0, len(ids))
	for _, id := range ids {
		cards = append(cards, *candidates[id].Card)
	}
	return cards
}

func sortedTopMatches(candidates map[string]*CardMatch, limit int) []CardMatch {
	matches := make([]CardMatch, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.Card != nil {
			matches = append(matches, *candidate)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].Card.ID < matches[j].Card.ID
	})
	if len(matches) > 1 && matches[0].Confidence-matches[1].Confidence <= 5 {
		matches[0].NeedsReview = true
		matches[1].NeedsReview = true
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func setDetectionStatus(result *DetectionResult) {
	if len(result.TopMatches) == 0 {
		result.Status = DetectionStatusNoMatch
		return
	}
	if result.TopMatches[0].NeedsReview {
		result.Status = DetectionStatusNeedsReview
		return
	}
	result.Status = DetectionStatusMatched
}
