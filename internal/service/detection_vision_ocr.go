package service

import (
	"context"
	"time"
)

// VisionOCRProvider returns complete, validated text or an error. Implementations
// must bound requests, reject truncated/repetitive output, and honor cancellation.
type VisionOCRProvider interface {
	Extract(context.Context, []byte) (string, error)
}

func strongOCRIdentity(candidate candidateEvidence) bool {
	return hasCandidateReason(candidate, "card_id", "set_and_collector") ||
		(hasCandidateReason(candidate, "collector_fraction") && hasCandidateReason(candidate, "exact_name", "localized_exact_name"))
}

func (p *DetectionPipeline) applyVisionOCR(ctx context.Context, request DetectionRequest, result *DetectionResult, candidates map[string]*CardMatch, local []candidateEvidence, scoped bool) bool {
	if p.VisionOCR == nil || !scoped {
		return false
	}
	// Preserve directly read identifiers, including fractions that do not set
	// the pipeline's narrower printingEvidence flag. Never override those.
	for _, candidate := range local {
		if strongOCRIdentity(candidate) {
			return false
		}
	}
	if hasHighConfidenceCandidate(candidates, 70) && !hasAmbiguousVisionCandidates(candidates) {
		return false
	}
	budget := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// Reserve response/fallback time; do not let a slow optional model turn
		// a useful local scan into a global request timeout.
		budget = min(budget, remaining-min(2*time.Second, remaining/2))
	}
	if budget <= 0 {
		return false
	}
	modelCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	started := time.Now()
	finish := LogScanStage(modelCtx, "vision_ocr")
	image, err := prepareLLMCardImage(modelCtx, request.Image)
	text := ""
	if err == nil {
		text, err = p.VisionOCR.Extract(modelCtx, image)
	}
	finish(err)
	result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{Name: "vision_ocr", Duration: time.Since(started), Error: err})
	if err != nil || modelCtx.Err() != nil {
		return false
	}
	// Rank the already scoped catalog, not arbitrary model IDs. Require BOTH
	// an exact (possibly localized) name and a strong collector identifier.
	// A name alone, bare number, invented ID or ambiguous printing abstains.
	var selected *candidateEvidence
	for _, candidate := range rankCandidates(text, request.Cards, len(request.Cards)) {
		if !hasCandidateReason(candidate, "exact_name", "localized_exact_name") || !strongOCRIdentity(candidate) {
			continue
		}
		if selected != nil {
			return false
		}
		copy := candidate
		selected = &copy
	}
	if selected == nil || !textSelectionPreservesIdentity(&selected.Card, local) {
		return false
	}
	match := getOrCreateMatch(candidates, &selected.Card)
	match.modelOCR = true
	if match.OCRScore == nil {
		match.OCRScore = &ConfidenceScore{Method: "vision_ocr", Score: 69, CardID: match.Card.ID, CardName: match.Card.Name, RawText: text}
	}
	// Preserve deterministic OCR text/score. Model output is not fed to the
	// text LLM again or counted as an independent corroborating signal.
	if !conflictingFingerprint(match, candidates) {
		match.textSelection = true
	}
	return true
}
