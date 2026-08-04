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
	"time"

	_ "golang.org/x/image/webp" // Register WebP decoder
)

// DetectionPipeline runs the full card detection pipeline with parallel
// fingerprint + OCR, confidence scoring, and metrics (SCAN-07, SCAN-09, SCAN-16).
type DetectionPipeline struct {
	// REFACTOR(step 3): migrate callers to a typed scan request while retaining
	// the current Detect and DetectContext compatibility methods.
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
