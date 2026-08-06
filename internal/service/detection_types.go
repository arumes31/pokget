package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"pokget/internal/models"
)

var (
	ErrInvalidDetectionRequest = errors.New("detection: invalid scoped request")
	ErrNoEligibleCards         = errors.New("detection: no active cards match the selected TCG and language")
)

// DetectionStatus is the machine-readable outcome of a pipeline run. Errors
// remain separate through DetectScoped's error return.
type DetectionStatus string

const (
	DetectionStatusUnknown        DetectionStatus = "unknown"
	DetectionStatusMatched        DetectionStatus = "matched"
	DetectionStatusNeedsReview    DetectionStatus = "needs_review"
	DetectionStatusNoMatch        DetectionStatus = "no_match"
	DetectionStatusInvalidRequest DetectionStatus = "invalid_request"
	DetectionStatusFailed         DetectionStatus = "failed"
	DetectionStatusCanceled       DetectionStatus = "canceled"
)

// ScanScope is selected by the user before scanning. It is also carried into
// fingerprint index selection so visually identical cards from another TCG or
// language cannot enter the candidate set.
type ScanScope struct {
	TCG                  models.TCG
	Language             models.Language
	FingerprintAlgorithm string
	FingerprintVersion   int
}

// DetectionRequest is the typed, catalog-scoped entry point for card scans.
type DetectionRequest struct {
	Image []byte
	Cards []models.Card
	Scope ScanScope
}

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
	Status         DetectionStatus
	TopMatches     []CardMatch
	Metrics        DetectionMetrics
	OCRText        string
	ProcessedImage []byte
}

// BestMatchID returns the stable printing ID of the top match.
func (r *DetectionResult) BestMatchID() string {
	if r == nil || len(r.TopMatches) == 0 || r.TopMatches[0].Card == nil {
		return ""
	}
	return r.TopMatches[0].Card.ID
}

// BestMatchName returns the name of the top match, or "Unknown Card" if none (SCAN-09).
func (r *DetectionResult) BestMatchName() string {
	if r == nil || len(r.TopMatches) == 0 || r.TopMatches[0].Card == nil {
		return "Unknown Card"
	}
	return r.TopMatches[0].Card.Name
}

// BestMatchConfidence returns the confidence of the top match (SCAN-09).
func (r *DetectionResult) BestMatchConfidence() float64 {
	if r == nil || len(r.TopMatches) == 0 {
		return 0
	}
	return r.TopMatches[0].Confidence
}

// BestMatchCard returns the top match card, or nil if none (SCAN-09).
func (r *DetectionResult) BestMatchCard() *models.Card {
	if r == nil || len(r.TopMatches) == 0 {
		return nil
	}
	return r.TopMatches[0].Card
}

// BestMatchNeedsReview returns true if the top match is below the confidence threshold (SCAN-09).
func (r *DetectionResult) BestMatchNeedsReview() bool {
	if r == nil || len(r.TopMatches) == 0 {
		return true
	}
	return r.TopMatches[0].NeedsReview
}
