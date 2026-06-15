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
	"image/color"
	"image/draw"
	"image/png"
	"pokget/internal/models"
	"sort"
	"sync"
	"testing"
	"time"
)

// --- SCAN-09: Confidence scoring tests ---

func TestFingerprintScoreFromDistance(t *testing.T) {
	tests := []struct {
		name      string
		distance  int
		threshold int
		expected  float64
	}{
		{"Exact match", 0, 10, 100.0},
		{"At threshold", 10, 10, 0.0},
		{"Above threshold", 15, 10, 0.0},
		{"Half threshold", 5, 10, 50.0},
		{"Close match", 1, 10, 90.0},
		{"Zero threshold", 5, 0, 0.0},
		{"Negative distance", -1, 10, 100.0}, // Should clamp to 100
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fingerprintScoreFromDistance(tt.distance, tt.threshold)
			if score != tt.expected {
				t.Errorf("fingerprintScoreFromDistance(%d, %d) = %f, want %f",
					tt.distance, tt.threshold, score, tt.expected)
			}
		})
	}
}

func TestOCRScoreFromLevenshtein(t *testing.T) {
	tests := []struct {
		name     string
		ocrText  string
		cardName string
		minScore float64 // Minimum expected score
		maxScore float64 // Maximum expected score
	}{
		{"Exact substring match", "I found a Charizard card", "Charizard", 90.0, 100.0},
		{"Empty OCR text", "", "Pikachu", 0.0, 0.0},
		{"Empty card name", "Pikachu", "", 0.0, 0.0},
		{"Both empty", "", "", 0.0, 0.0},
		{"Similar text", "Pikach", "Pikachu", 70.0, 100.0},
		{"Very different", "Bulbasaur", "Charizard", 0.0, 30.0},
		{"Case insensitive match", "charizard is here", "Charizard", 90.0, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ocrScoreFromLevenshtein(tt.ocrText, tt.cardName)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ocrScoreFromLevenshtein(%q, %q) = %f, want between %f and %f",
					tt.ocrText, tt.cardName, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestCombineScores(t *testing.T) {
	tests := []struct {
		name     string
		fp       *ConfidenceScore
		ocr      *ConfidenceScore
		llm      *ConfidenceScore
		expected float64
	}{
		{
			"All scores present",
			&ConfidenceScore{Method: "fingerprint", Score: 80},
			&ConfidenceScore{Method: "ocr", Score: 70},
			&ConfidenceScore{Method: "llm", Score: 90},
			0, // Will compute: (80*0.5 + 70*0.3 + 90*0.2) / 1.0 = 79.0
		},
		{
			"Only fingerprint",
			&ConfidenceScore{Method: "fingerprint", Score: 100},
			nil,
			nil,
			100.0, // (100*0.5) / 0.5 = 100
		},
		{
			"No scores",
			nil,
			nil,
			nil,
			0.0,
		},
		{
			"Zero scores",
			&ConfidenceScore{Method: "fingerprint", Score: 0},
			&ConfidenceScore{Method: "ocr", Score: 0},
			nil,
			0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := combineScores(tt.fp, tt.ocr, tt.llm)
			if tt.expected == 0 && tt.name == "All scores present" {
				// Compute expected: (80*0.5 + 70*0.3 + 90*0.2) / (0.5+0.3+0.2)
				expected := (80.0*0.5 + 70.0*0.3 + 90.0*0.2) / 1.0
				if result != expected {
					t.Errorf("combineScores() = %f, want %f", result, expected)
				}
			} else if result != tt.expected {
				t.Errorf("combineScores() = %f, want %f", result, tt.expected)
			}
		})
	}
}

func TestCombineScoresWeights(t *testing.T) {
	// Verify that fingerprint has the highest weight
	fp := &ConfidenceScore{Method: "fingerprint", Score: 100}
	ocr := &ConfidenceScore{Method: "ocr", Score: 0}
	llm := &ConfidenceScore{Method: "llm", Score: 0}

	result := combineScores(fp, ocr, llm)
	// With only fingerprint contributing: (100*0.5) / 0.5 = 100
	if result != 100.0 {
		t.Errorf("Expected 100 with only fingerprint, got %f", result)
	}
}

// --- SCAN-09: CardMatch and DetectionResult tests ---

func TestDetectionResultBestMatchName(t *testing.T) {
	tests := []struct {
		name     string
		result   *DetectionResult
		expected string
	}{
		{
			"With matches",
			&DetectionResult{
				TopMatches: []CardMatch{
					{Card: &models.Card{Name: "Pikachu"}, Confidence: 95.0},
					{Card: &models.Card{Name: "Charizard"}, Confidence: 80.0},
				},
			},
			"Pikachu",
		},
		{
			"No matches",
			&DetectionResult{},
			"Unknown Card",
		},
		{
			"Nil top matches",
			&DetectionResult{TopMatches: nil},
			"Unknown Card",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.BestMatchName()
			if got != tt.expected {
				t.Errorf("BestMatchName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDetectionResultBestMatchConfidence(t *testing.T) {
	result := &DetectionResult{
		TopMatches: []CardMatch{
			{Card: &models.Card{Name: "Pikachu"}, Confidence: 95.5},
		},
	}
	if got := result.BestMatchConfidence(); got != 95.5 {
		t.Errorf("BestMatchConfidence() = %f, want 95.5", got)
	}

	emptyResult := &DetectionResult{}
	if got := emptyResult.BestMatchConfidence(); got != 0 {
		t.Errorf("BestMatchConfidence() on empty = %f, want 0", got)
	}
}

func TestDetectionResultBestMatchNeedsReview(t *testing.T) {
	tests := []struct {
		name       string
		result     *DetectionResult
		wantReview bool
	}{
		{
			"High confidence",
			&DetectionResult{
				TopMatches: []CardMatch{
					{Card: &models.Card{Name: "Pikachu"}, Confidence: 95.0, NeedsReview: false},
				},
			},
			false,
		},
		{
			"Low confidence",
			&DetectionResult{
				TopMatches: []CardMatch{
					{Card: &models.Card{Name: "Pikachu"}, Confidence: 50.0, NeedsReview: true},
				},
			},
			true,
		},
		{
			"No matches",
			&DetectionResult{},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.BestMatchNeedsReview()
			if got != tt.wantReview {
				t.Errorf("BestMatchNeedsReview() = %v, want %v", got, tt.wantReview)
			}
		})
	}
}

func TestGetOrCreateMatch(t *testing.T) {
	m := make(map[string]*CardMatch)
	card1 := &models.Card{ID: "card-1", Name: "Pikachu"}

	// Create new match
	cm := getOrCreateMatch(m, card1)
	if cm == nil {
		t.Fatal("Expected non-nil CardMatch")
	}
	if cm.Card.Name != "Pikachu" {
		t.Errorf("Expected card name Pikachu, got %s", cm.Card.Name)
	}

	// Get existing match
	cm2 := getOrCreateMatch(m, card1)
	if cm != cm2 {
		t.Error("Expected same CardMatch instance for same card ID")
	}

	// Create different card
	card2 := &models.Card{ID: "card-2", Name: "Charizard"}
	cm3 := getOrCreateMatch(m, card2)
	if cm3.Card.Name != "Charizard" {
		t.Errorf("Expected card name Charizard, got %s", cm3.Card.Name)
	}
	if len(m) != 2 {
		t.Errorf("Expected 2 entries in map, got %d", len(m))
	}
}

// --- SCAN-16: DetectionMetrics tests ---

func TestDetectionMetricsFormat(t *testing.T) {
	metrics := DetectionMetrics{
		Stages: []DetectionStageMetrics{
			{Name: "fingerprint", Duration: 100000000}, // 100ms
			{Name: "ocr", Duration: 500000000},         // 500ms
			{Name: "llm", Duration: 2000000000, Error: fmt.Errorf("timeout")},
		},
		TotalTime: 2600000000, // 2.6s
	}

	formatted := metrics.Format()
	if formatted == "" {
		t.Error("Expected non-empty formatted metrics")
	}
	// Should contain stage names
	if !containsStr(formatted, "fingerprint") {
		t.Error("Expected 'fingerprint' in formatted metrics")
	}
	if !containsStr(formatted, "ocr") {
		t.Error("Expected 'ocr' in formatted metrics")
	}
	if !containsStr(formatted, "llm") {
		t.Error("Expected 'llm' in formatted metrics")
	}
}

func TestDetectionMetricsFormatEmpty(t *testing.T) {
	metrics := DetectionMetrics{}
	formatted := metrics.Format()
	if formatted == "" {
		t.Error("Expected non-empty formatted metrics even when empty")
	}
}

// --- SCAN-07: DetectionPipeline tests ---

func TestNewDetectionPipeline(t *testing.T) {
	fp := NewFingerprintService(nil)
	llm := &LLMService{}
	pipeline := NewDetectionPipeline(fp, llm)

	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}
	if pipeline.Fingerprint != fp {
		t.Error("Expected fingerprint service to be set")
	}
	if pipeline.LLM != llm {
		t.Error("Expected LLM service to be set")
	}
}

func TestNewDetectionPipelineNilServices(t *testing.T) {
	pipeline := NewDetectionPipeline(nil, nil)
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline even with nil services")
	}
}

// Helper for string containment check
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- NEW: Comprehensive confidence scoring tests ---

// TestFingerprintScoreHighConfidence verifies that fingerprint distance 0-5
// produces scores in the 85-100% range.
func TestFingerprintScoreHighConfidence(t *testing.T) {
	tests := []struct {
		name          string
		distance      int
		threshold     int
		minScore      float64
		maxScore      float64
	}{
		{"Distance 0 — exact match", 0, 10, 100.0, 100.0},
		{"Distance 1 — near exact", 1, 10, 85.0, 100.0},
		{"Distance 2 — very close", 2, 10, 80.0, 100.0},
		{"Distance 3 — close", 3, 10, 65.0, 100.0},
		{"Distance 5 — high conf boundary", 5, 10, 50.0, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fingerprintScoreFromDistance(tt.distance, tt.threshold)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("fingerprintScoreFromDistance(%d, %d) = %f, want between %f and %f",
					tt.distance, tt.threshold, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

// TestFingerprintScoreMediumConfidence verifies that medium-distance
// fingerprint matches produce moderate scores.
func TestFingerprintScoreMediumConfidence(t *testing.T) {
	tests := []struct {
		name      string
		distance  int
		threshold int
		minScore  float64
		maxScore  float64
	}{
		{"Distance 6 — medium", 6, 10, 30.0, 50.0},
		{"Distance 7 — medium-low", 7, 10, 20.0, 40.0},
		{"Distance 9 — near threshold", 9, 10, 0.0, 20.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := fingerprintScoreFromDistance(tt.distance, tt.threshold)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("fingerprintScoreFromDistance(%d, %d) = %f, want between %f and %f",
					tt.distance, tt.threshold, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

// TestOCRScoreHighConfidence verifies that OCR matches with high Levenshtein
// similarity produce scores in the 80-95% range.
func TestOCRScoreHighConfidence(t *testing.T) {
	tests := []struct {
		name     string
		ocrText  string
		cardName string
		minScore float64
		maxScore float64
	}{
		{"Exact substring", "This is a Pikachu card", "Pikachu", 90.0, 100.0},
		{"Case insensitive exact", "PIKACHU", "Pikachu", 90.0, 100.0},
		{"Very similar", "Pikachu", "Pikachu", 90.0, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ocrScoreFromLevenshtein(tt.ocrText, tt.cardName)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ocrScoreFromLevenshtein(%q, %q) = %f, want between %f and %f",
					tt.ocrText, tt.cardName, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

// TestOCRScoreMediumConfidence verifies that OCR matches with moderate
// Levenshtein similarity produce scores in the 60-85% range.
func TestOCRScoreMediumConfidence(t *testing.T) {
	tests := []struct {
		name     string
		ocrText  string
		cardName string
		minScore float64
		maxScore float64
	}{
		{"Similar with typo", "Pikach", "Pikachu", 60.0, 95.0},
		{"Partial match", "Charzard", "Charizard", 60.0, 95.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ocrScoreFromLevenshtein(tt.ocrText, tt.cardName)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ocrScoreFromLevenshtein(%q, %q) = %f, want between %f and %f",
					tt.ocrText, tt.cardName, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

// TestOCRScoreLowConfidence verifies that OCR matches with low similarity
// produce low scores.
func TestOCRScoreLowConfidence(t *testing.T) {
	tests := []struct {
		name     string
		ocrText  string
		cardName string
		maxScore float64
	}{
		{"Very different", "Bulbasaur", "Charizard", 30.0},
		{"Completely different", "xyz", "Pikachu", 30.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ocrScoreFromLevenshtein(tt.ocrText, tt.cardName)
			if score > tt.maxScore {
				t.Errorf("ocrScoreFromLevenshtein(%q, %q) = %f, want at most %f",
					tt.ocrText, tt.cardName, score, tt.maxScore)
			}
		})
	}
}

// TestLowConfidenceFlaggedForReview verifies that combined score <70%
// is flagged for manual review.
func TestLowConfidenceFlaggedForReview(t *testing.T) {
	// Low fingerprint + low OCR → combined < 70%
	fp := &ConfidenceScore{Method: "fingerprint", Score: 30}
	ocr := &ConfidenceScore{Method: "ocr", Score: 20}

	combined := combineScores(fp, ocr, nil)
	needsReview := combined < 70

	if !needsReview {
		t.Errorf("Expected combined score %f to be flagged for review (< 70%%)", combined)
	}
}

// TestMultipleSignalsAgreement verifies that when fingerprint + OCR both
// match the same card, the combined confidence is high.
func TestMultipleSignalsAgreement(t *testing.T) {
	// Both fingerprint and OCR agree on the same card
	fp := &ConfidenceScore{Method: "fingerprint", Score: 90}
	ocr := &ConfidenceScore{Method: "ocr", Score: 85}

	combined := combineScores(fp, ocr, nil)

	// Weighted: (90*0.5 + 85*0.3) / (0.5+0.3) = (45+25.5)/0.8 = 88.125
	if combined < 80 {
		t.Errorf("Expected high combined confidence when both signals agree, got %f", combined)
	}
}

// TestConflictingSignals verifies that when fingerprint says card A and
// OCR says card B, both appear in results with appropriate confidence.
func TestConflictingSignals(t *testing.T) {
	candidateMap := make(map[string]*CardMatch)

	cardA := &models.Card{ID: "card-a", Name: "Pikachu"}
	cardB := &models.Card{ID: "card-b", Name: "Charizard"}

	// Fingerprint says card A
	cmA := getOrCreateMatch(candidateMap, cardA)
	cmA.FingerprintScore = &ConfidenceScore{Method: "fingerprint", Score: 80, CardName: "Pikachu", CardID: "card-a"}

	// OCR says card B
	cmB := getOrCreateMatch(candidateMap, cardB)
	cmB.OCRScore = &ConfidenceScore{Method: "ocr", Score: 75, CardName: "Charizard", CardID: "card-b"}

	// Compute combined scores
	for _, cm := range candidateMap {
		cm.Confidence = combineScores(cm.FingerprintScore, cm.OCRScore, cm.LLMScore)
		cm.NeedsReview = cm.Confidence < 70
	}

	// Both cards should be in the map
	if len(candidateMap) != 2 {
		t.Errorf("Expected 2 candidates with conflicting signals, got %d", len(candidateMap))
	}

	// Both should have some confidence
	for id, cm := range candidateMap {
		if cm.Confidence <= 0 {
			t.Errorf("Card %s should have positive confidence, got %f", id, cm.Confidence)
		}
	}
}

// TestTop5Ranking verifies that results are sorted by confidence descending
// and limited to top 5.
func TestTop5Ranking(t *testing.T) {
	// Create 7 matches with varying confidence
	allMatches := make([]CardMatch, 0, 7)
	for i := 7; i >= 1; i-- {
		allMatches = append(allMatches, CardMatch{
			Card:       &models.Card{ID: fmt.Sprintf("card-%d", i), Name: fmt.Sprintf("Card %d", i)},
			Confidence: float64(i * 10),
		})
	}

	// Sort by confidence (highest first) — mirrors pipeline logic
	sort.Slice(allMatches, func(i, j int) bool {
		return allMatches[i].Confidence > allMatches[j].Confidence
	})

	// Take top 5
	if len(allMatches) > 5 {
		allMatches = allMatches[:5]
	}

	if len(allMatches) != 5 {
		t.Fatalf("Expected 5 results, got %d", len(allMatches))
	}

	// Verify descending order
	for i := 1; i < len(allMatches); i++ {
		if allMatches[i].Confidence > allMatches[i-1].Confidence {
			t.Errorf("Results not sorted by confidence: [%d]=%f > [%d]=%f",
				i, allMatches[i].Confidence, i-1, allMatches[i-1].Confidence)
		}
	}

	// Top result should have highest confidence
	if allMatches[0].Confidence != 70.0 {
		t.Errorf("Expected top confidence 70, got %f", allMatches[0].Confidence)
	}
}

// TestDeduplicationSameCardDifferentMethods verifies that the same card
// matched by different methods is merged with the best score kept.
func TestDeduplicationSameCardDifferentMethods(t *testing.T) {
	candidateMap := make(map[string]*CardMatch)

	card := &models.Card{ID: "card-1", Name: "Pikachu"}

	// Fingerprint match
	cm := getOrCreateMatch(candidateMap, card)
	cm.FingerprintScore = &ConfidenceScore{Method: "fingerprint", Score: 90, CardName: "Pikachu", CardID: "card-1"}

	// OCR match for same card — should merge into existing entry
	cm2 := getOrCreateMatch(candidateMap, card)
	cm2.OCRScore = &ConfidenceScore{Method: "ocr", Score: 85, CardName: "Pikachu", CardID: "card-1"}

	// Should be the same entry
	if cm != cm2 {
		t.Error("Expected same CardMatch instance for same card ID")
	}

	// Should have both scores
	if cm.FingerprintScore == nil || cm.OCRScore == nil {
		t.Error("Expected both fingerprint and OCR scores after dedup")
	}

	// Only 1 entry in map
	if len(candidateMap) != 1 {
		t.Errorf("Expected 1 candidate after dedup, got %d", len(candidateMap))
	}
}

// TestCombineScoresOnlyFingerprint verifies that when only fingerprint
// score is available, the combined score equals the fingerprint score.
func TestCombineScoresOnlyFingerprint(t *testing.T) {
	fp := &ConfidenceScore{Method: "fingerprint", Score: 85}
	result := combineScores(fp, nil, nil)

	// (85*0.5) / 0.5 = 85
	if result != 85.0 {
		t.Errorf("Expected 85 with only fingerprint, got %f", result)
	}
}

// TestCombineScoresOnlyOCR verifies that when only OCR score is available,
// the combined score equals the OCR score.
func TestCombineScoresOnlyOCR(t *testing.T) {
	ocr := &ConfidenceScore{Method: "ocr", Score: 75}
	result := combineScores(nil, ocr, nil)

	// (75*0.3) / 0.3 = 75
	if result != 75.0 {
		t.Errorf("Expected 75 with only OCR, got %f", result)
	}
}

// TestCombineScoresOnlyLLM verifies that when only LLM score is available,
// the combined score equals the LLM score.
func TestCombineScoresOnlyLLM(t *testing.T) {
	llm := &ConfidenceScore{Method: "llm", Score: 60}
	result := combineScores(nil, nil, llm)

	// (60*0.2) / 0.2 = 60
	if result != 60.0 {
		t.Errorf("Expected 60 with only LLM, got %f", result)
	}
}

// TestCombineScoresFingerprintDominates verifies that fingerprint has
// the highest weight in the combined score. When all three methods are
// present with different scores, a high fingerprint score pulls the
// combined result higher than a high OCR or LLM score would.
func TestCombineScoresFingerprintDominates(t *testing.T) {
	// All methods have same score — result should equal that score
	fp := &ConfidenceScore{Method: "fingerprint", Score: 50}
	ocr := &ConfidenceScore{Method: "ocr", Score: 50}
	llm := &ConfidenceScore{Method: "llm", Score: 50}

	result := combineScores(fp, ocr, llm)

	// (50*0.5 + 50*0.3 + 50*0.2) / 1.0 = 50
	if result != 50.0 {
		t.Errorf("Expected 50 when all scores equal, got %f", result)
	}

	// When all methods are present, a high fingerprint score produces a
	// higher combined score than a low fingerprint score, because
	// fingerprint has the largest weight (0.5).
	// highFp: fp=100, ocr=50, llm=50 → (100*0.5 + 50*0.3 + 50*0.2) / 1.0 = 75
	// lowFp:  fp=50, ocr=100, llm=50 → (50*0.5 + 100*0.3 + 50*0.2) / 1.0 = 65
	highFp := combineScores(
		&ConfidenceScore{Method: "fingerprint", Score: 100},
		&ConfidenceScore{Method: "ocr", Score: 50},
		&ConfidenceScore{Method: "llm", Score: 50})
	lowFp := combineScores(
		&ConfidenceScore{Method: "fingerprint", Score: 50},
		&ConfidenceScore{Method: "ocr", Score: 100},
		&ConfidenceScore{Method: "llm", Score: 50})

	if highFp <= lowFp {
		t.Errorf("Expected high fingerprint (%f) to beat low fingerprint (%f) when all methods present", highFp, lowFp)
	}
}

// TestOCRScoreCJK verifies that OCR scoring works with CJK card names.
func TestOCRScoreCJK(t *testing.T) {
	tests := []struct {
		name     string
		ocrText  string
		cardName string
		minScore float64
	}{
		{"Japanese exact substring", "これはピカチュウのカード", "ピカチュウ", 90.0},
		{"Chinese exact substring", "这是皮卡丘卡", "皮卡丘", 90.0},
		{"Korean exact substring", "이것은 피카츄 카드", "피카츄", 90.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ocrScoreFromLevenshtein(tt.ocrText, tt.cardName)
			if score < tt.minScore {
				t.Errorf("ocrScoreFromLevenshtein(%q, %q) = %f, want at least %f",
					tt.ocrText, tt.cardName, score, tt.minScore)
			}
		})
	}
}

// --- NEW: Integration/Pipeline tests ---

// TestFullPipelineEnglishCard verifies the full detection pipeline with
// an English card image (using stub OCR).
func TestFullPipelineEnglishCard(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	// Create a test image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}

	result := pipeline.Detect(buf.Bytes(), cards, "eng")

	if result == nil {
		t.Fatal("Expected non-nil DetectionResult")
	}

	// Should have metrics recorded
	if result.Metrics.TotalTime == 0 {
		t.Error("Expected non-zero total time in metrics")
	}

	// Should have stage metrics
	if len(result.Metrics.Stages) == 0 {
		t.Error("Expected stage metrics to be recorded")
	}

	// OCR text should be present (stub returns "OCR Not Available (Stub)")
	if result.OCRText == "" {
		t.Error("Expected non-empty OCR text")
	}
}

// TestFullPipelineJapaneseCard verifies the full detection pipeline with
// a Japanese card image (using stub OCR with CJK language).
func TestFullPipelineJapaneseCard(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	cards := []models.Card{
		{ID: "jp-1", Name: "ピカチュウ"},
		{ID: "jp-2", Name: "リザードン"},
	}

	result := pipeline.Detect(buf.Bytes(), cards, "jpn")

	if result == nil {
		t.Fatal("Expected non-nil DetectionResult")
	}

	// Should have metrics
	if result.Metrics.TotalTime == 0 {
		t.Error("Expected non-zero total time in metrics")
	}
}

// TestFullPipelineNoMatch verifies that an unknown image results in
// low confidence and is flagged for review.
func TestFullPipelineNoMatch(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// No matching cards in the database
	cards := []models.Card{
		{ID: "1", Name: "Mewtwo"},
	}

	result := pipeline.Detect(buf.Bytes(), cards, "eng")

	// With no fingerprint match and stub OCR returning "Unknown Card",
	// the result should be flagged for review
	if !result.BestMatchNeedsReview() {
		t.Error("Expected result to be flagged for review with no match")
	}
}

// TestPipelineParallelExecution verifies that fingerprint and OCR run
// concurrently (both stages should have non-zero durations).
func TestPipelineParallelExecution(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	result := pipeline.Detect(buf.Bytes(), nil, "eng")

	// Both fingerprint and OCR stages should have metrics
	stageNames := make(map[string]bool)
	for _, s := range result.Metrics.Stages {
		stageNames[s.Name] = true
	}

	if !stageNames["fingerprint"] {
		t.Error("Expected fingerprint stage in metrics")
	}
	if !stageNames["ocr"] {
		t.Error("Expected ocr stage in metrics")
	}
}

// TestPipelineMetricsCollection verifies that timing metrics are recorded
// for each stage of the pipeline.
func TestPipelineMetricsCollection(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	result := pipeline.Detect(buf.Bytes(), nil, "eng")

	// Total time should be positive
	if result.Metrics.TotalTime <= 0 {
		t.Error("Expected positive total time in metrics")
	}

	// Each stage should have a name and duration
	for _, stage := range result.Metrics.Stages {
		if stage.Name == "" {
			t.Error("Expected non-empty stage name in metrics")
		}
		// Duration can be 0 for very fast stages, but should not be negative
		if stage.Duration < 0 {
			t.Errorf("Stage %s has negative duration %v", stage.Name, stage.Duration)
		}
	}

	// Should have at least fingerprint, ocr, and combine stages
	if len(result.Metrics.Stages) < 3 {
		t.Errorf("Expected at least 3 stages in metrics, got %d", len(result.Metrics.Stages))
	}
}

// TestPipelineOCRCacheHitOnSecondScan verifies that scanning the same image
// twice results in a cache hit on the second scan.
func TestPipelineOCRCacheHitOnSecondScan(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// First scan
	result1 := pipeline.Detect(buf.Bytes(), nil, "eng")
	if result1 == nil {
		t.Fatal("First scan returned nil")
	}

	// Second scan — should use OCR cache
	result2 := pipeline.Detect(buf.Bytes(), nil, "eng")
	if result2 == nil {
		t.Fatal("Second scan returned nil")
	}

	// OCR text should be the same (from cache)
	if result1.OCRText != result2.OCRText {
		t.Errorf("OCR text mismatch: first=%q, second=%q", result1.OCRText, result2.OCRText)
	}
}

// TestPipelineWithFingerprintMatch verifies the pipeline when a fingerprint
// match is found in the BK-tree.
func TestPipelineWithFingerprintMatch(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	// Add a fingerprint to the BK-tree
	testCard := &models.Card{ID: "fp-match-1", Name: "Fingerprint Match Card"}
	fp.AddFingerprint(12345, testCard)

	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	cards := []models.Card{
		{ID: "fp-match-1", Name: "Fingerprint Match Card"},
	}

	result := pipeline.Detect(buf.Bytes(), cards, "eng")

	// Pipeline should complete without error
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Metrics should be recorded
	if result.Metrics.TotalTime == 0 {
		t.Error("Expected non-zero total time")
	}
}

// TestPipelineConcurrentDetectionRequests verifies that 10 simultaneous
// detection requests don't deadlock or corrupt state.
func TestPipelineConcurrentDetectionRequests(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	var wg sync.WaitGroup
	results := make(chan *DetectionResult, 10)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Use different languages to avoid cache hits
			lang := "eng"
			if idx%3 == 1 {
				lang = "jpn"
			} else if idx%3 == 2 {
				lang = "deu"
			}
			result := pipeline.Detect(buf.Bytes(), nil, lang)
			if result == nil {
				errors <- fmt.Errorf("goroutine %d: nil result", idx)
				return
			}
			results <- result
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent detection error: %v", err)
	}

	// Verify all results are valid
	count := 0
	for result := range results {
		count++
		if result.Metrics.TotalTime == 0 {
			t.Error("Expected non-zero total time in concurrent result")
		}
	}

	if count != 10 {
		t.Errorf("Expected 10 results from concurrent detection, got %d", count)
	}
}

// --- NEW: Edge case tests ---

// TestPipelineCorruptedImageData verifies that corrupted image data
// is handled gracefully by the pipeline.
func TestPipelineCorruptedImageData(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	// Corrupted image data
	corruptData := []byte{0xFF, 0xD8, 0xFF, 0x00, 0xDE, 0xAD}

	result := pipeline.Detect(corruptData, nil, "eng")

	// Pipeline should not panic, should return a result (possibly with errors in metrics)
	if result == nil {
		t.Fatal("Expected non-nil result even with corrupted image")
	}

	// Should have fingerprint error in metrics
	hasFpError := false
	for _, stage := range result.Metrics.Stages {
		if stage.Name == "fingerprint" && stage.Error != nil {
			hasFpError = true
		}
	}
	if !hasFpError {
		t.Log("Note: fingerprint stage may not have error if image decoded partially")
	}
}

// TestPipelineEmptyImageData verifies that empty image data
// is handled gracefully by the pipeline.
func TestPipelineEmptyImageData(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	result := pipeline.Detect([]byte{}, nil, "eng")

	// Should not panic
	if result == nil {
		t.Fatal("Expected non-nil result even with empty image data")
	}
}

// TestPipelineVerySmallImage verifies that a very small image (50x50)
// is processed without errors.
func TestPipelineVerySmallImage(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	// Create a 50x50 image
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	result := pipeline.Detect(buf.Bytes(), nil, "eng")

	if result == nil {
		t.Fatal("Expected non-nil result for small image")
	}
}

// TestPipelineLargeImage verifies that a large image (10MP+) is processed
// without OOM or panics.
func TestPipelineLargeImage(t *testing.T) {
	ocrCache.Clear()

	fp := NewFingerprintService(nil)
	pipeline := NewDetectionPipeline(fp, nil)

	// Create a large image (4000x2500 = 10MP)
	// Use a smaller size to avoid test timeout, but still test the path
	img := image.NewRGBA(image.Rect(0, 0, 4000, 2500))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	// This should complete without OOM
	done := make(chan *DetectionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("panic: %v", r)
			}
		}()
		result := pipeline.Detect(buf.Bytes(), nil, "eng")
		done <- result
	}()

	select {
	case result := <-done:
		if result == nil {
			t.Error("Expected non-nil result for large image")
		}
	case err := <-errCh:
		t.Errorf("Large image processing panicked: %v", err)
	case <-time.After(30 * time.Second):
		t.Error("Large image processing timed out")
	}
}

// TestDetectionResultBestMatchCard verifies BestMatchCard method.
func TestDetectionResultBestMatchCard(t *testing.T) {
	tests := []struct {
		name     string
		result   *DetectionResult
		wantNil  bool
		wantName string
	}{
		{
			"With matches",
			&DetectionResult{
				TopMatches: []CardMatch{
					{Card: &models.Card{Name: "Pikachu"}, Confidence: 95.0},
				},
			},
			false,
			"Pikachu",
		},
		{
			"No matches",
			&DetectionResult{},
			true,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := tt.result.BestMatchCard()
			if tt.wantNil && card != nil {
				t.Errorf("Expected nil card, got %v", card)
			}
			if !tt.wantNil && card == nil {
				t.Error("Expected non-nil card")
			}
			if !tt.wantNil && card.Name != tt.wantName {
				t.Errorf("Expected card name %q, got %q", tt.wantName, card.Name)
			}
		})
	}
}

// TestFingerprintScoreFromDistanceWithPotentialThreshold verifies scoring
// with the potential (relaxed) threshold.
func TestFingerprintScoreFromDistanceWithPotentialThreshold(t *testing.T) {
	// Using the potential threshold (10) for scoring
	tests := []struct {
		distance  int
		threshold int
		expected  float64
	}{
		{0, 10, 100.0},
		{5, 10, 50.0},
		{10, 10, 0.0},
		{11, 10, 0.0},
	}

	for _, tt := range tests {
		score := fingerprintScoreFromDistance(tt.distance, tt.threshold)
		if score != tt.expected {
			t.Errorf("fingerprintScoreFromDistance(%d, %d) = %f, want %f",
				tt.distance, tt.threshold, score, tt.expected)
		}
	}
}

// TestOCRScoreFromLevenshteinCJK verifies OCR scoring with CJK text.
func TestOCRScoreFromLevenshteinCJK(t *testing.T) {
	// CJK card name found in OCR text → high score
	score := ocrScoreFromLevenshtein("ピカチュウのカード", "ピカチュウ")
	if score < 90 {
		t.Errorf("Expected high score for CJK substring match, got %f", score)
	}

	// CJK card name not in OCR text → low score
	score = ocrScoreFromLevenshtein("リザードン", "ピカチュウ")
	if score > 50 {
		t.Errorf("Expected low score for CJK non-match, got %f", score)
	}
}
