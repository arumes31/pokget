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
	"fmt"
	"image"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"log/slog"
	"pokget/internal/models"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "golang.org/x/image/webp" // Register WebP decoder
)

// DetectionStageMetrics holds timing metrics for a single detection stage (SCAN-16).
type DetectionStageMetrics struct {
	Name     string
	Duration time.Duration
	Error    error
}

// DetectionMetrics holds timing metrics for the entire detection pipeline (SCAN-16).
type DetectionMetrics struct {
	Stages    []DetectionStageMetrics
	TotalTime time.Duration
}

// Format returns a human-readable summary of the metrics (SCAN-16).
func (m *DetectionMetrics) Format() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Total: %v", m.TotalTime))
	for _, s := range m.Stages {
		if s.Error != nil {
			b.WriteString(fmt.Sprintf(" | %s: %v (err: %v)", s.Name, s.Duration, s.Error))
		} else {
			b.WriteString(fmt.Sprintf(" | %s: %v", s.Name, s.Duration))
		}
	}
	return b.String()
}

// ConfidenceScore represents a 0-100 confidence score from a detection method (SCAN-09).
type ConfidenceScore struct {
	Method   string // "fingerprint", "ocr", "llm"
	Score    float64
	CardName string
	CardID   string
	Distance int    // For fingerprint: Hamming distance
	RawText  string // For OCR: the matched text
}

// CardMatch represents a ranked match result with combined confidence (SCAN-09).
type CardMatch struct {
	Card             *models.Card
	Confidence       float64
	FingerprintScore *ConfidenceScore
	OCRScore         *ConfidenceScore
	LLMScore         *ConfidenceScore
	NeedsReview      bool // Flag for low-confidence results (SCAN-09)
}

// DetectionResult is the output of the full detection pipeline (SCAN-07, SCAN-09, SCAN-16).
type DetectionResult struct {
	TopMatches     []CardMatch
	Metrics        DetectionMetrics
	OCRText        string
	ProcessedImage []byte
}

// fingerprintScoreFromDistance converts a Hamming distance to a 0-100 confidence score (SCAN-09).
// Distance 0 = 100%, distance >= threshold = 0%.
func fingerprintScoreFromDistance(distance int, threshold int) float64 {
	if distance >= threshold {
		return 0
	}
	if distance <= 0 {
		return 100
	}
	// Linear interpolation: 0 distance = 100%, threshold distance = 0%
	return float64(threshold-distance) / float64(threshold) * 100
}

// ocrScoreFromLevenshtein converts a Levenshtein similarity to a 0-100 score (SCAN-09).
func ocrScoreFromLevenshtein(ocrText, cardName string) float64 {
	if ocrText == "" || cardName == "" {
		return 0
	}
	ocrLower := strings.ToLower(ocrText)
	nameLower := strings.ToLower(cardName)

	// Very short Latin names (for example Pokémon "N") must match a complete
	// token; substring matching would otherwise accept almost any OCR text.
	// CJK scripts do not generally use whitespace token boundaries, so their
	// short names still use substring matching.
	shortLatinName := len([]rune(nameLower)) <= 3
	for _, r := range nameLower {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			shortLatinName = false
			break
		}
	}
	if shortLatinName {
		for _, token := range strings.FieldsFunc(ocrLower, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}) {
			if token == nameLower {
				return 95
			}
		}
	} else if strings.Contains(ocrLower, nameLower) {
		return 95.0
	}

	// Use Levenshtein distance for similarity
	maxLen := len([]rune(ocrLower))
	nameRunes := []rune(nameLower)
	if len(nameRunes) > maxLen {
		maxLen = len(nameRunes)
	}
	if maxLen == 0 {
		return 0
	}

	dist := levenshtein(ocrLower, nameLower)
	similarity := float64(maxLen-dist) / float64(maxLen) * 100
	if similarity < 0 {
		similarity = 0
	}
	return similarity
}

// combineScores merges confidence scores from multiple detection methods (SCAN-09).
// Weights: fingerprint=0.5, OCR=0.3, LLM=0.2
func combineScores(fp *ConfidenceScore, ocr *ConfidenceScore, llm *ConfidenceScore) float64 {
	totalWeight := 0.0
	weightedSum := 0.0
	signalCount := 0

	if fp != nil && fp.Score > 0 {
		weightedSum += fp.Score * 0.5
		totalWeight += 0.5
		signalCount++
	}
	if ocr != nil && ocr.Score > 0 {
		weightedSum += ocr.Score * 0.3
		totalWeight += 0.3
		signalCount++
	}
	if llm != nil && llm.Score > 0 {
		weightedSum += llm.Score * 0.2
		totalWeight += 0.2
		signalCount++
	}

	if totalWeight == 0 {
		return 0
	}

	// Independent signals agreeing on one candidate corroborate each other, so
	// compare their weighted average. Keep a lone signal's reliability weight
	// fixed; renormalizing it made OCR-only and ambiguous fingerprint matches
	// look definitive.
	if signalCount > 1 {
		return weightedSum / totalWeight
	}
	return weightedSum
}

// DetectionPipeline runs the full card detection pipeline with parallel
// fingerprint + OCR, confidence scoring, and metrics (SCAN-07, SCAN-09, SCAN-16).
type DetectionPipeline struct {
	Fingerprint *FingerprintService
	LLM         *LLMService
}

// NewDetectionPipeline creates a new detection pipeline (SCAN-07).
func NewDetectionPipeline(fingerprint *FingerprintService, llm *LLMService) *DetectionPipeline {
	return &DetectionPipeline{
		Fingerprint: fingerprint,
		LLM:         llm,
	}
}

// Detect runs the full detection pipeline on a card image (SCAN-07, SCAN-09, SCAN-16).
func (p *DetectionPipeline) Detect(imgBytes []byte, cards []models.Card, lang string) *DetectionResult {
	return p.DetectContext(context.Background(), imgBytes, cards, lang)
}

// DetectContext runs card detection and stops cancellable OCR/LLM work with ctx.
func (p *DetectionPipeline) DetectContext(ctx context.Context, imgBytes []byte, cards []models.Card, lang string) *DetectionResult {
	totalStart := time.Now()
	result := &DetectionResult{}
	if err := ctx.Err(); err != nil {
		result.Metrics.TotalTime = time.Since(totalStart)
		result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "context", Error: err})
		return result
	}
	fingerprintCards := slices.Clone(cards)
	ocrCards := slices.Clone(cards)

	// --- Stage 1: Fingerprint matching ---
	var fpResult *MatchResult
	var fpErr error
	fpStart := time.Now()
	if p.Fingerprint != nil {
		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			fpErr = fmt.Errorf("fingerprint: failed to decode image: %w", err)
		} else {
			hash, err := p.Fingerprint.CalculateHash(img)
			if err != nil {
				fpErr = fmt.Errorf("fingerprint: failed to calculate hash: %w", err)
			} else {
				fpResult = p.Fingerprint.SearchByHashWithCards(hash, fingerprintCards)
			}
		}
	}
	fpDuration := time.Since(fpStart)
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "fingerprint", Duration: fpDuration, Error: fpErr},
	)

	// An exact pHash match outranks merely nearby hashes. This avoids expensive
	// OCR when one stored card exactly matches even if visually similar cards are
	// also inside the relaxed threshold. Multiple exact matches remain ambiguous.
	if matches := exactFingerprintMatches(fpResult); len(matches) > 0 {
		needsReview := len(matches) > 1
		confidence := 100.0
		if needsReview {
			confidence = 50
		}
		result.TopMatches = make([]CardMatch, 0, len(matches))
		for _, match := range matches {
			result.TopMatches = append(result.TopMatches, CardMatch{
				Card: match.Card,
				FingerprintScore: &ConfidenceScore{
					Method: "fingerprint", Score: 100, CardName: match.Card.Name, CardID: match.Card.ID, Distance: 0,
				},
				Confidence:  confidence,
				NeedsReview: needsReview,
			})
		}
		result.Metrics.TotalTime = time.Since(totalStart)
		if needsReview {
			slog.Info("Detection: Exact fingerprint collision requires review", "matches", len(matches), "metrics", result.Metrics.Format())
		} else {
			slog.Info("Detection: Unique exact fingerprint match", "metrics", result.Metrics.Format(),
				"top_match", matches[0].Card.Name, "confidence", confidence)
		}
		return result
	}

	// A unique near match inside the strict visual threshold is deterministic
	// enough to avoid OCR. Near collisions still need secondary verification.
	highConfidenceThreshold := DefaultPhashThresholdHighConf
	if p.Fingerprint != nil {
		highConfidenceThreshold = p.Fingerprint.PhashHighConf
	}
	if exact, distance := uniqueHighConfidenceFingerprint(fpResult, highConfidenceThreshold); exact != nil {
		confidence := max(75, 100-float64(distance*5))
		result.TopMatches = []CardMatch{{
			Card: exact,
			FingerprintScore: &ConfidenceScore{
				Method: "fingerprint", Score: confidence, CardName: exact.Name, CardID: exact.ID, Distance: distance,
			},
			Confidence: confidence,
		}}
		result.Metrics.TotalTime = time.Since(totalStart)
		slog.Info("Detection: Unique high-confidence fingerprint match", "metrics", result.Metrics.Format(),
			"top_match", exact.Name, "confidence", confidence, "distance", distance)
		return result
	}
	if matches := ambiguousSameNameFingerprints(fpResult, highConfidenceThreshold); len(matches) > 1 {
		result.TopMatches = make([]CardMatch, 0, len(matches))
		for _, match := range matches {
			confidence := 0.5 * fingerprintScoreFromDistance(match.Distance, p.Fingerprint.PhashHighConf)
			result.TopMatches = append(result.TopMatches, CardMatch{
				Card: match.Card,
				FingerprintScore: &ConfidenceScore{
					Method: "fingerprint", Score: confidence * 2, CardName: match.Card.Name, CardID: match.Card.ID, Distance: match.Distance,
				},
				Confidence:  confidence,
				NeedsReview: true,
			})
		}
		result.Metrics.TotalTime = time.Since(totalStart)
		slog.Info("Detection: Ambiguous fingerprint collision requires review", "matches", len(matches), "metrics", result.Metrics.Format())
		return result
	}

	// --- Stage 2: OCR for ambiguous or non-exact visual matches ---
	ocrStart := time.Now()
	ocrText, ocrDetectedCard, ocrProcessedImg, ocrErr := ProcessCardScanContext(ctx, imgBytes, ocrCards, lang, p.LLM)
	ocrDuration := time.Since(ocrStart)
	result.ProcessedImage = ocrProcessedImg
	result.OCRText = ocrText

	result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "ocr", Duration: ocrDuration, Error: ocrErr})
	if err := ctx.Err(); err != nil {
		result.Metrics.TotalTime = time.Since(totalStart)
		return result
	}

	// --- Stage 3: Combine results and compute confidence scores (SCAN-09) ---
	combineStart := time.Now()

	// Collect all candidate cards with their scores
	candidateMap := make(map[string]*CardMatch)

	// Process fingerprint results
	if fpResult != nil {
		if fpResult.HighConfidence != nil {
			card := fpResult.HighConfidence
			score := fingerprintScoreFromDistance(fpResult.BestDistance, p.Fingerprint.PhashHighConf)
			cm := getOrCreateMatch(candidateMap, card)
			cm.FingerprintScore = &ConfidenceScore{
				Method:   "fingerprint",
				Score:    score,
				CardName: card.Name,
				CardID:   card.ID,
				Distance: fpResult.BestDistance,
			}
		}

		// Add potential matches (need secondary verification)
		for _, m := range fpResult.Potential {
			score := fingerprintScoreFromDistance(m.Distance, p.Fingerprint.PhashPotential)
			cm := getOrCreateMatch(candidateMap, m.Card)
			if cm.FingerprintScore == nil {
				cm.FingerprintScore = &ConfidenceScore{
					Method:   "fingerprint",
					Score:    score,
					CardName: m.Card.Name,
					CardID:   m.Card.ID,
					Distance: m.Distance,
				}
			}
		}
	}

	// Process OCR result
	if ocrDetectedCard != "" && ocrDetectedCard != "Unknown Card" {
		matchingIndexes := make([]int, 0, 1)
		for i := range cards {
			if cards[i].ID == ocrDetectedCard {
				matchingIndexes = []int{i}
				break
			}
			if cards[i].Name == ocrDetectedCard {
				matchingIndexes = append(matchingIndexes, i)
			}
		}
		// A name-only OCR result cannot select arbitrarily among duplicate printings.
		if len(matchingIndexes) == 1 {
			c := &cards[matchingIndexes[0]]
			score := ocrScoreFromLevenshtein(ocrText, c.Name)
			cm := getOrCreateMatch(candidateMap, c)
			cm.OCRScore = &ConfidenceScore{
				Method:   "ocr",
				Score:    score,
				CardName: c.Name,
				CardID:   c.ID,
				RawText:  ocrText,
			}
		}
	}

	// --- Stage 4: LLM verification for low-confidence or potential matches (SCAN-08) ---
	var llmDuration time.Duration
	if p.LLM != nil && len(candidateMap) > 0 {
		// Compute combined confidence scores before checking so hasHighConf
		// evaluates already-computed values, not zero defaults.
		for _, cm := range candidateMap {
			cm.Confidence = combineScores(cm.FingerprintScore, cm.OCRScore, cm.LLMScore)
		}

		// Only run LLM if no high-confidence match found
		hasHighConf := false
		for _, cm := range candidateMap {
			if cm.Confidence >= 70 {
				hasHighConf = true
				break
			}
		}

		if !hasHighConf {
			llmStart := time.Now()
			llmResp, llmErr := p.LLM.FuzzyMatchCardWithValidationContext(ctx, ocrText, cards)
			llmDuration = time.Since(llmStart)

			if llmErr == nil && llmResp != nil && llmResp.CardName != "Unknown Card" {
				for _, c := range cards {
					if c.Name == llmResp.CardName {
						cm := getOrCreateMatch(candidateMap, &c)
						cm.LLMScore = &ConfidenceScore{
							Method:   "llm",
							Score:    llmResp.Confidence * 100,
							CardName: c.Name,
							CardID:   c.ID,
						}
						break
					}
				}
			}

			result.Metrics.Stages = append(result.Metrics.Stages,
				DetectionStageMetrics{Name: "llm", Duration: llmDuration, Error: llmErr},
			)
		}
	}

	// Compute (or recompute) combined confidence scores (SCAN-09)
	for _, cm := range candidateMap {
		cm.Confidence = combineScores(cm.FingerprintScore, cm.OCRScore, cm.LLMScore)
		cm.NeedsReview = cm.Confidence < 70 // SCAN-09: Flag low-confidence results
	}

	// Sort by confidence (highest first) and take top 5 (SCAN-09)
	allMatches := make([]CardMatch, 0, len(candidateMap))
	for _, cm := range candidateMap {
		allMatches = append(allMatches, *cm)
	}
	sort.Slice(allMatches, func(i, j int) bool {
		if allMatches[i].Confidence != allMatches[j].Confidence {
			return allMatches[i].Confidence > allMatches[j].Confidence
		}
		return allMatches[i].Card.ID < allMatches[j].Card.ID
	})
	if len(allMatches) > 1 && allMatches[0].Confidence-allMatches[1].Confidence <= 5 {
		allMatches[0].NeedsReview = true
		allMatches[1].NeedsReview = true
	}

	if len(allMatches) > 5 {
		allMatches = allMatches[:5]
	}
	result.TopMatches = allMatches

	combineDuration := time.Since(combineStart)
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "combine", Duration: combineDuration},
	)

	result.Metrics.TotalTime = time.Since(totalStart)

	// Log metrics (SCAN-16)
	slog.Info("Detection: Pipeline complete", "metrics", result.Metrics.Format(),
		"top_match", result.BestMatchName(), "confidence", result.BestMatchConfidence())

	return result
}

func ambiguousSameNameFingerprints(result *MatchResult, threshold int) []FingerprintMatch {
	if result == nil {
		return nil
	}
	matches := make([]FingerprintMatch, 0, len(result.Potential))
	name := ""
	for _, match := range result.Potential {
		if match.Distance > threshold {
			break
		}
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(match.Card.Name))
		}
		if strings.ToLower(strings.TrimSpace(match.Card.Name)) != name {
			return nil
		}
		matches = append(matches, match)
	}
	return matches
}

func exactFingerprintMatches(result *MatchResult) []FingerprintMatch {
	if result == nil {
		return nil
	}
	matches := make([]FingerprintMatch, 0, 1)
	seen := make(map[string]bool)
	for _, match := range result.Potential {
		if match.Distance > 0 {
			break
		}
		if match.Card == nil || seen[match.Card.ID] {
			continue
		}
		seen[match.Card.ID] = true
		matches = append(matches, match)
	}
	return matches
}

func uniqueHighConfidenceFingerprint(result *MatchResult, threshold int) (*models.Card, int) {
	if result == nil {
		return nil, 0
	}
	var candidate *models.Card
	distance := 0
	for _, match := range result.Potential {
		if match.Distance > threshold {
			break
		}
		if candidate != nil && candidate.ID != match.Card.ID {
			return nil, 0
		}
		candidate = match.Card
		distance = match.Distance
	}
	return candidate, distance
}

// BestMatchName returns the name of the top match, or "Unknown Card" if none (SCAN-09).
func (r *DetectionResult) BestMatchName() string {
	if len(r.TopMatches) == 0 {
		return "Unknown Card"
	}
	return r.TopMatches[0].Card.Name
}

// BestMatchConfidence returns the confidence of the top match (SCAN-09).
func (r *DetectionResult) BestMatchConfidence() float64 {
	if len(r.TopMatches) == 0 {
		return 0
	}
	return r.TopMatches[0].Confidence
}

// BestMatchCard returns the top match card, or nil if none (SCAN-09).
func (r *DetectionResult) BestMatchCard() *models.Card {
	if len(r.TopMatches) == 0 {
		return nil
	}
	return r.TopMatches[0].Card
}

// BestMatchNeedsReview returns true if the top match is below the confidence threshold (SCAN-09).
func (r *DetectionResult) BestMatchNeedsReview() bool {
	if len(r.TopMatches) == 0 {
		return true
	}
	return r.TopMatches[0].NeedsReview
}

// getOrCreateMatch gets an existing CardMatch from the map or creates a new one (SCAN-09).
func getOrCreateMatch(m map[string]*CardMatch, card *models.Card) *CardMatch {
	if cm, ok := m[card.ID]; ok {
		return cm
	}
	cm := &CardMatch{Card: card}
	m[card.ID] = cm
	return cm
}
