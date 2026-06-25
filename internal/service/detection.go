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
	"fmt"
	"image"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"log/slog"
	"pokget/internal/models"
	"sort"
	"strings"
	"sync"
	"time"

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

	// If the card name is found in the OCR text, high confidence
	if strings.Contains(ocrLower, nameLower) {
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

	if fp != nil && fp.Score > 0 {
		weightedSum += fp.Score * 0.5
		totalWeight += 0.5
	}
	if ocr != nil && ocr.Score > 0 {
		weightedSum += ocr.Score * 0.3
		totalWeight += 0.3
	}
	if llm != nil && llm.Score > 0 {
		weightedSum += llm.Score * 0.2
		totalWeight += 0.2
	}

	if totalWeight == 0 {
		return 0
	}

	return weightedSum / totalWeight
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
	totalStart := time.Now()
	result := &DetectionResult{}

	// --- Stage 1: Fingerprint matching (parallel with OCR) ---
	var fpResult *MatchResult
	var fpErr error
	var fpDuration time.Duration

	// --- Stage 2: OCR (parallel with fingerprint) ---
	var ocrText string
	var ocrDetectedCard string
	var ocrProcessedImg []byte
	var ocrErr error
	var ocrDuration time.Duration

	var wg sync.WaitGroup
	wg.Add(2)

	// Run fingerprint matching in parallel (SCAN-07)
	go func() {
		defer wg.Done()
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
					fpResult = p.Fingerprint.SearchByHashWithCards(hash, cards)
				}
			}
		}
		fpDuration = time.Since(fpStart)
	}()

	// Run OCR in parallel (SCAN-07)
	go func() {
		defer wg.Done()
		ocrStart := time.Now()
		ocrText, ocrDetectedCard, ocrProcessedImg, ocrErr = ProcessCardScan(imgBytes, cards, lang, p.LLM)
		ocrDuration = time.Since(ocrStart)
	}()

	wg.Wait()

	result.ProcessedImage = ocrProcessedImg
	result.OCRText = ocrText

	// Record stage metrics (SCAN-16)
	result.Metrics.Stages = append(result.Metrics.Stages,
		DetectionStageMetrics{Name: "fingerprint", Duration: fpDuration, Error: fpErr},
		DetectionStageMetrics{Name: "ocr", Duration: ocrDuration, Error: ocrErr},
	)

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
			if cm.FingerprintScore == nil || score > cm.FingerprintScore.Score {
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
		for _, c := range cards {
			if c.Name == ocrDetectedCard || c.ID == ocrDetectedCard {
				score := ocrScoreFromLevenshtein(ocrText, c.Name)
				cm := getOrCreateMatch(candidateMap, &c)
				cm.OCRScore = &ConfidenceScore{
					Method:   "ocr",
					Score:    score,
					CardName: c.Name,
					CardID:   c.ID,
					RawText:  ocrText,
				}
				break
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
			llmResp, llmErr := p.LLM.FuzzyMatchCardWithValidation(ocrText, cards)
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
		return allMatches[i].Confidence > allMatches[j].Confidence
	})

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
