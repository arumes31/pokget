package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"pokget/internal/models"
)

// MaxDeviceOCRTextBytes bounds untrusted transcription before catalog matching.
const MaxDeviceOCRTextBytes = 8 << 10

func deviceTextNeedsLLM(candidates []candidateEvidence) bool {
	if len(candidates) < 2 || uniqueOCRPrintingID(candidates) != "" {
		return false
	}
	identities := 0
	for _, candidate := range candidates {
		if !hasCandidateReason(candidate, "collector_fraction") {
			continue
		}
		for _, reason := range candidate.Reasons {
			if strings.HasSuffix(reason, "_name") {
				identities++
				break
			}
		}
	}
	if identities == 1 {
		return false
	}
	// A name-only model cannot identify the set of identical-name printings.
	// Keep those alternatives reviewable without paying for an arbitrary pick.
	name := normalizeMatchText(candidates[0].Card.Name)
	for _, candidate := range candidates[1:] {
		if normalizeMatchText(candidate.Card.Name) != name {
			return true
		}
	}
	return false
}

// TextDetectionRequest contains observations, never client-selected card IDs,
// confidence scores, or catalog candidates.
type TextDetectionRequest struct {
	Text  string
	Cards []models.Card
	Scope ScanScope
}

// DetectTextScoped skips image decoding, fingerprinting and server OCR. Results
// always require review because the server has not inspected the source image.
func (p *DetectionPipeline) DetectTextScoped(ctx context.Context, request TextDetectionRequest) (*DetectionResult, error) {
	started := time.Now()
	if ctx == nil {
		return invalidDetectionResult(started), ErrInvalidDetectionRequest
	}
	if err := ctx.Err(); err != nil {
		return failedDetectionResult(err), err
	}
	if len(request.Text) > MaxDeviceOCRTextBytes || !utf8.ValidString(request.Text) || strings.ContainsRune(request.Text, '\x00') || strings.TrimSpace(request.Text) == "" {
		return invalidDetectionResult(started), fmt.Errorf("%w: invalid device text", ErrInvalidDetectionRequest)
	}
	if !request.Scope.TCG.Valid() || !request.Scope.Language.Valid() {
		return invalidDetectionResult(started), ErrInvalidDetectionRequest
	}
	cards := cardsForScope(request.Cards, request.Scope)
	ScanLogger(ctx).Info("Scan scope selected", "game", request.Scope.TCG, "language", request.Scope.Language, "cached_cards", len(request.Cards), "eligible_cards", len(cards))
	if len(cards) == 0 {
		return invalidDetectionResult(started), errors.Join(ErrInvalidDetectionRequest, ErrNoEligibleCards)
	}
	// This path is local-only even if an administrator also configured a remote
	finish := LogScanStage(ctx, "device_text_matching")
	// vision provider for image scans. Do not mutate the shared pipeline/client.
	local := *p
	local.VisionOCR = nil
	if p.LLM != nil {
		llm := *p.LLM
		llm.PrimaryBaseURL = ""
		llm.compactSelectionIDs = true
		local.LLM = &llm
	}
	result, err := local.combineDetection(ctx, DetectionRequest{Cards: cards, Scope: request.Scope},
		&DetectionResult{OCRText: request.Text}, fingerprintStageOutput{}, ocrStageOutput{text: request.Text}, true, started)
	finish(err)
	if result != nil {
		for i := range result.TopMatches {
			result.TopMatches[i].Confidence = min(result.TopMatches[i].Confidence, 69)
			result.TopMatches[i].NeedsReview = true
		}
		if err == nil {
			setDetectionStatus(result)
		}
	}
	return result, err
}
