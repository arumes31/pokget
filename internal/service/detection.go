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
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"pokget/internal/models"
)

type fingerprintStageRunner func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error)
type ocrStageRunner func(context.Context, []byte, []models.Card, string) (string, string, []byte, error)

// DetectionPipeline runs fingerprint and OCR independently, combines their
// deterministic evidence, and uses an LLM only to choose a shortlisted ID.
type DetectionPipeline struct {
	Fingerprint *FingerprintService
	LLM         *LLMService
	VisionOCR   VisionOCRProvider // Optional image transcription; nil keeps local-only behavior.

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
	if timeout := detectionStageTimeoutFromContext(ctx); timeout > 0 {
		cancel()
		stageCtx, cancel = context.WithTimeout(ctx, timeout)
	}
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
		case <-stageCtx.Done():
			cancel()
			result.Status = DetectionStatusCanceled
			result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "context", Error: stageCtx.Err()})
			result.Metrics.TotalTime = time.Since(totalStart)
			return result, stageCtx.Err()
		}
	}
	result.OCRText = ocrOutput.text
	result.ProcessedImage = ocrOutput.processedImage
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "fingerprint", Duration: fingerprintOutput.duration, Error: fingerprintOutput.err},
		DetectionStageMetrics{Name: "ocr", Duration: ocrOutput.duration, Error: ocrOutput.err},
	)
	if err := stageCtx.Err(); err != nil {
		result.Status = DetectionStatusCanceled
		result.Metrics.TotalTime = time.Since(totalStart)
		return result, err
	}

	fingerprintResult := fingerprintOutput.result

	combineStart := time.Now()
	candidateMap := make(map[string]*CardMatch)
	addFingerprintCandidates(candidateMap, fingerprintResult, p.Fingerprint)
	ocrCandidates := resolveOCRCandidates(ocrOutput.detectedCardID, ocrOutput.text, request.Cards)
	printingID := uniqueOCRPrintingID(ocrCandidates)
	for _, candidate := range ocrCandidates {
		card := cardByID(request.Cards, candidate.Card.ID)
		if card == nil {
			continue
		}
		// Name evidence identifies the card family; printed identifiers must
		// retain a higher score so a name substring cannot erase that detail.
		score := min(99, float64(50+candidate.Score/25))
		match := getOrCreateMatch(candidateMap, card)
		match.printingEvidence = card.ID == printingID
		match.OCRScore = &ConfidenceScore{
			Method: "ocr", Score: score, CardName: card.Name, CardID: card.ID, RawText: ocrOutput.text,
		}
	}

	for _, match := range candidateMap {
		match.Confidence = combineScores(match.FingerprintScore, match.OCRScore, match.LLMScore)
	}
	visionOCRSelected := p.applyVisionOCR(ctx, request, result, candidateMap, ocrCandidates, scoped)
	if err := ctx.Err(); err != nil {
		result.Status = DetectionStatusCanceled
		result.Metrics.TotalTime = time.Since(totalStart)
		return result, err
	}
	if !visionOCRSelected && p.LLM != nil && (!hasHighConfidenceCandidate(candidateMap, 70) || (p.LLM.PrimaryBaseURL != "" && hasAmbiguousVisionCandidates(candidateMap))) {
		llmCards := candidateCards(candidateMap)
		if len(llmCards) > 0 {
			llmStart := time.Now()
			var llmResponse *LLMCardResponse
			var llmErr error
			var llmImage []byte
			llmCtx := ctx
			cancelLLM := func() {}
			if p.LLM.PrimaryBaseURL == "" {
				// Optional text disambiguation must leave time to return local
				// evidence for review, even when the model is cold or stalled.
				budget := 5 * time.Second
				if deadline, ok := ctx.Deadline(); ok {
					budget = min(budget, time.Until(deadline)/2)
				}
				llmCtx, cancelLLM = context.WithTimeout(ctx, budget)
			}
			if p.LLM.PrimaryBaseURL != "" {
				var imageErr error
				llmImage, imageErr = prepareLLMCardImage(ctx, request.Image)
				if imageErr != nil {
					llmImage = ocrOutput.processedImage
				}
			}
			if scoped {
				llmResponse, llmErr = p.LLM.fuzzyMatchCardScopedWithArtworkContext(llmCtx, ocrOutput.text, llmImage, llmCards, request.Cards, request.Scope)
			} else {
				llmResponse, llmErr = p.LLM.fuzzyMatchCardWithArtworkContext(llmCtx, ocrOutput.text, llmImage, llmCards, request.Cards)
			}
			cancelLLM()
			result.Metrics.Stages = append(result.Metrics.Stages,
				DetectionStageMetrics{Name: "llm", Duration: time.Since(llmStart), Error: llmErr},
			)
			if err := ctx.Err(); err != nil {
				result.Status = DetectionStatusCanceled
				result.Metrics.TotalTime = time.Since(totalStart)
				return result, err
			}
			if llmErr == nil && llmResponse != nil && !llmResponse.Abstained && llmResponse.CardID != "" {
				if match := candidateMap[llmResponse.CardID]; match != nil {
					if llmResponse.visionSelected {
						match.visionSelection = true
						match.LLMScore = &ConfidenceScore{Method: "llm", Score: llmResponse.Confidence * 100, CardName: match.Card.Name, CardID: match.Card.ID}
					} else if textSelectionPreservesIdentity(match.Card, ocrCandidates) && !conflictingFingerprint(match, candidateMap) {
						match.textSelection = true
					}
				}
			}
		}
	}

	ambiguousNames := ambiguousPrintingNames(request.Cards)
	for _, match := range candidateMap {
		match.Confidence = combineScores(match.FingerprintScore, match.OCRScore, match.LLMScore)
		match.NeedsReview = match.Confidence < 70
		if match.modelOCR {
			// Model-derived OCR is not an independent deterministic signal.
			match.Confidence = min(match.Confidence, 69)
			match.NeedsReview = true
		}
		if !match.printingEvidence && ambiguousNames[normalizeMatchText(match.Card.Name)] {
			match.NeedsReview = true
		}
		if match.printingEvidence && conflictingFingerprint(match, candidateMap) {
			match.NeedsReview = true
		}
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
	highConfidenceThreshold := DefaultPhashThresholdHighConf
	if p.Fingerprint != nil {
		highConfidenceThreshold = p.Fingerprint.PhashHighConf
	}
	// Perceptual hashes discard small printing details. Only an isolated
	// exact hash may skip OCR; near matches and collisions need both stages.
	if exact, _ := uniqueHighConfidenceFingerprint(fingerprintResult, highConfidenceThreshold); len(matches) == 1 && exact != nil {
		setExactFingerprintResult(result, matches)
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
	config := ocrScanConfigFromContext(ctx)
	decoded, _, err := decodeOCRImage(imageBytes, config)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: decode image: %w", err)
	}
	decoded = applyEXIFOrientation(imageBytes, decoded)
	if config.GuideCrop != nil {
		decoded, err = cropNormalized(decoded, *config.GuideCrop)
		if err != nil {
			return nil, fmt.Errorf("fingerprint: crop card region: %w", err)
		}
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
	potentialThreshold := DefaultPhashThresholdPotential
	if fingerprint != nil {
		potentialThreshold = fingerprint.PhashPotential
	}
	if result.HighConfidence != nil {
		card := result.HighConfidence
		match := getOrCreateMatch(candidateMap, card)
		if match == nil {
			return
		}
		match.FingerprintScore = &ConfidenceScore{
			Method: "fingerprint", Score: fingerprintScoreFromDistance(result.BestDistance, potentialThreshold),
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
	// The OCR runner's ID is a suggested candidate, often chosen from a name
	// tie. Score the actual text across the catalog instead of treating that
	// suggestion as an identifier visibly printed on the card.
	ranked := rankCandidates(ocrText, cards, min(10, len(cards)))
	// A fuzzy OCR name can still be useful when the full-text ranker has no
	// exact name token. Carry its entire printing family at review confidence.
	name := normalizeMatchText(detected)
	if suggested := cardByID(cards, detected); suggested != nil {
		name = normalizeMatchText(suggested.Name)
	}
	if name != "" && name != "unknown card" {
		for _, card := range cards {
			if normalizeMatchText(card.Name) != name && !localizedNameMatches(card, name) {
				continue
			}
			if !fuzzySubstringMatch(normalizeMatchText(ocrText), name) {
				continue
			}
			index := slices.IndexFunc(ranked, func(candidate candidateEvidence) bool { return candidate.Card.ID == card.ID })
			if index < 0 {
				ranked = append(ranked, candidateEvidence{Card: card, Score: defaultLLMMinEvidence, Reasons: []string{"fuzzy_name_hint"}})
			} else if ranked[index].Score < defaultLLMMinEvidence {
				ranked[index].Score = defaultLLMMinEvidence
				ranked[index].Reasons = append(ranked[index].Reasons, "fuzzy_name_hint")
			}
		}
		sort.Slice(ranked, func(i, j int) bool { return betterCandidateEvidence(ranked[i], ranked[j]) })
		if len(ranked) > 10 {
			ranked = ranked[:10]
		}
	}
	matched := ranked[:0]
	for _, candidate := range ranked {
		if candidate.Score >= defaultLLMMinEvidence {
			matched = append(matched, candidate)
		}
	}
	return matched
}

func uniqueOCRPrintingID(candidates []candidateEvidence) string {
	id := ""
	for _, candidate := range candidates {
		if !slices.Contains(candidate.Reasons, "card_id") && !slices.Contains(candidate.Reasons, "set_and_collector") {
			continue
		}
		if id != "" && id != candidate.Card.ID {
			return ""
		}
		id = candidate.Card.ID
	}
	return id
}

func textSelectionPreservesIdentity(selected *models.Card, candidates []candidateEvidence) bool {
	hasIdentity := false
	for _, candidate := range candidates {
		printedID := hasCandidateReason(candidate, "card_id", "set_and_collector")
		strong := printedID
		if !strong && hasCandidateReason(candidate, "collector_fraction") {
			for _, reason := range candidate.Reasons {
				strong = strong || strings.HasSuffix(reason, "_name")
			}
		}
		if !strong {
			continue
		}
		hasIdentity = true
		if candidate.Card.ID == selected.ID || (!printedID && normalizeMatchText(candidate.Card.Name) == normalizeMatchText(selected.Name) && collectorNumberKey(candidate.Card.CollectorNumber) != "" && collectorNumberKey(candidate.Card.CollectorNumber) == collectorNumberKey(selected.CollectorNumber)) {
			return true
		}
	}
	return !hasIdentity
}

func ambiguousPrintingNames(cards []models.Card) map[string]bool {
	firstID := make(map[string]string, len(cards))
	ambiguous := make(map[string]bool)
	for _, card := range cards {
		names := append([]string{card.Name}, card.LocalizedNames...)
		for _, value := range names {
			name := normalizeMatchText(value)
			if name == "" {
				continue
			}
			if id, exists := firstID[name]; exists && id != card.ID {
				ambiguous[name] = true
			} else {
				firstID[name] = card.ID
			}
		}
	}
	return ambiguous
}

func conflictingFingerprint(match *CardMatch, candidates map[string]*CardMatch) bool {
	for _, other := range candidates {
		if other.Card.ID == match.Card.ID || other.FingerprintScore == nil {
			continue
		}
		if match.FingerprintScore == nil || other.FingerprintScore.Distance+2 < match.FingerprintScore.Distance {
			return true
		}
	}
	return false
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

// Vision can help rank nearby printings even when name OCR and similar artwork
// jointly produce a high score. A unique printed identifier remains authoritative.
func hasAmbiguousVisionCandidates(candidates map[string]*CardMatch) bool {
	ranked := sortedTopMatches(candidates, 2)
	if len(ranked) < 2 || ranked[0].printingEvidence {
		return false
	}
	name := normalizeMatchText(ranked[0].Card.Name)
	for _, candidate := range candidates {
		if candidate == nil || candidate.Card == nil {
			continue
		}
		if candidate.Card.ID != ranked[0].Card.ID && normalizeMatchText(candidate.Card.Name) == name && candidate.Confidence >= ranked[0].Confidence-10 {
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
		if matches[i].printingEvidence != matches[j].printingEvidence {
			return matches[i].printingEvidence
		}
		leftVision := matches[i].visionSelection && matches[i].NeedsReview
		rightVision := matches[j].visionSelection && matches[j].NeedsReview
		if leftVision != rightVision {
			return leftVision
		}
		leftText := matches[i].textSelection && matches[i].NeedsReview
		rightText := matches[j].textSelection && matches[j].NeedsReview
		if leftText != rightText {
			return leftText
		}
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].Card.ID < matches[j].Card.ID
	})
	if len(matches) > 1 && !matches[0].printingEvidence && matches[0].Confidence-matches[1].Confidence <= 5 {
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
