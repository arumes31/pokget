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
	"sort"
	"strings"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

// ClaudeDetector is the interface for Claude API vision card recognition.
type ClaudeDetector interface {
	Detect(ctx context.Context, imageBytes []byte, gameHint, langHint string) (*claudedetect.DetectedCardInfo, error)
	Model() string
	PromptCachingEnabled() bool
}

// detectClaude runs Claude vision card detection, replacing legacy detections.
func (p *DetectionPipeline) detectClaude(ctx context.Context, request DetectionRequest, scoped bool, totalStart time.Time) (*DetectionResult, error) {
	result := &DetectionResult{Status: DetectionStatusUnknown}
	started := time.Now()
	finish := LogScanStage(ctx, "claude_detection")

	gameHint := ""
	langHint := ""
	if scoped {
		gameHint = string(request.Scope.TCG)
		langHint = string(request.Scope.Language)
	}

	info, err := p.Claude.Detect(ctx, request.Image, gameHint, langHint)
	finish(err)

	result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{
		Name:     "claude_detection",
		Duration: time.Since(started),
		Error:    err,
	})

	if err != nil {
		ScanLogger(ctx).Warn("Claude detection returned error", "error", err)
		return nil, err
	}

	result.ProcessedImage = request.Image
	result.OCRText = formatClaudeText(info)

	// Match extracted card details against candidate catalog
	matches := matchCardsWithClaude(info, request.Cards)
	result.TopMatches = matches
	result.Metrics.TotalTime = time.Since(totalStart)

	if len(matches) > 0 {
		top := matches[0]
		// A synthetic card (ID == "") does not represent a resolved catalog match and requires review.
		if top.Card != nil && top.Card.ID != "" && top.Confidence >= 70 && !top.NeedsReview {
			result.Status = DetectionStatusMatched
		} else {
			result.Status = DetectionStatusNeedsReview
		}
	} else {
		result.Status = DetectionStatusNoMatch
	}

	ScanLogger(ctx).Info("Claude detection complete",
		"status", result.Status,
		"model", info.ModelUsed,
		"cache_read_tokens", info.CacheReadTokens,
		"cache_created_tokens", info.CacheCreationTokens,
		"top_match", result.BestMatchName(),
		"confidence", result.BestMatchConfidence())

	return result, nil
}

func formatClaudeText(info *claudedetect.DetectedCardInfo) string {
	parts := []string{info.Name}
	if info.CollectorNumber != "" {
		parts = append(parts, info.CollectorNumber)
	}
	if info.Set != "" {
		parts = append(parts, info.Set)
	}
	if info.Language != "" {
		parts = append(parts, "["+info.Language+"]")
	}
	if info.Variant != "" {
		parts = append(parts, "("+info.Variant+")")
	}
	return strings.Join(parts, " ")
}

// matchCardsWithClaude scores and ranks cards in the catalog based on Claude's extraction.
func matchCardsWithClaude(info *claudedetect.DetectedCardInfo, cards []models.Card) []CardMatch {
	if info == nil {
		return nil
	}

	type scoredMatch struct {
		card        models.Card
		confidence  float64
		needsReview bool
	}

	var candidates []scoredMatch
	normClaudeName := normalizeMatchText(info.Name)
	claudeCollector := normalizeCollectorNum(info.CollectorNumber)
	claudeSet := normalizeMatchText(info.Set)
	claudeSetCode := normalizeMatchText(info.SetCode)

	for _, card := range cards {
		if !card.IsCatalogActive() {
			continue
		}

		cardNormName := normalizeMatchText(card.Name)
		cardCollector := normalizeCollectorNum(card.CollectorNumber)
		cardSet := normalizeMatchText(card.Set)
		cardSetCode := normalizeMatchText(card.SetCode)

		// Name matching (check primary and localized names)
		nameMatched := cardNormName == normClaudeName
		if !nameMatched {
			for _, loc := range card.LocalizedNames {
				if normalizeMatchText(loc) == normClaudeName {
					nameMatched = true
					break
				}
			}
		}

		// Partial / substring name matching
		partialName := false
		if !nameMatched && normClaudeName != "" && cardNormName != "" {
			if strings.Contains(cardNormName, normClaudeName) || strings.Contains(normClaudeName, cardNormName) {
				partialName = true
			}
		}

		// Collector number matching
		collectorMatched := false
		partialCollector := false
		if claudeCollector != "" && cardCollector != "" {
			k1 := collectorNumberKey(claudeCollector)
			k2 := collectorNumberKey(cardCollector)
			hasSlash1 := strings.Contains(claudeCollector, "/")
			hasSlash2 := strings.Contains(cardCollector, "/")

			if hasSlash1 && hasSlash2 {
				parts1 := strings.SplitN(claudeCollector, "/", 2)
				parts2 := strings.SplitN(cardCollector, "/", 2)
				num1 := collectorNumberKey(parts1[0])
				num2 := collectorNumberKey(parts2[0])
				den1 := collectorNumberKey(parts1[1])
				den2 := collectorNumberKey(parts2[1])
				if num1 != "" && num1 == num2 && den1 != "" && den1 == den2 {
					collectorMatched = true
				} else if num1 != "" && num1 == num2 {
					// Numerator matches but different denominator -> different printing/set!
					partialCollector = true
				}
			} else if hasSlash1 || hasSlash2 {
				// One has a fraction and the other is just a number
				cNum1 := extractNumerator(claudeCollector)
				cNum2 := extractNumerator(cardCollector)
				kNum1 := collectorNumberKey(cNum1)
				kNum2 := collectorNumberKey(cNum2)
				if kNum1 != "" && kNum1 == kNum2 {
					partialCollector = true
				}
			} else {
				// Neither has fraction: compare keys (strips leading zeroes)
				if (k1 != "" && k1 == k2) || claudeCollector == cardCollector {
					collectorMatched = true
				}
			}
		}

		// Set / SetCode matching
		setMatched := false
		if claudeSetCode != "" && cardSetCode != "" && claudeSetCode == cardSetCode {
			setMatched = true
		} else if claudeSet != "" && cardSet != "" {
			if claudeSet == cardSet || strings.Contains(cardSet, claudeSet) || strings.Contains(claudeSet, cardSet) {
				setMatched = true
			}
		}

		// Scoring logic
		var score float64
		needsReview := false

		if nameMatched && collectorMatched {
			// Exact name + exact collector number = definitive match!
			score = 98.0
			if setMatched {
				score = 99.0
			}
			needsReview = false
		} else if nameMatched && partialCollector {
			if setMatched {
				score = 94.0
				needsReview = false
			} else {
				score = 86.0
				needsReview = true
			}
		} else if nameMatched && setMatched {
			score = 92.0
			needsReview = false
		} else if collectorMatched && setMatched {
			score = 88.0
			needsReview = true
		} else if nameMatched {
			score = 75.0
			needsReview = true
		} else if partialName && collectorMatched {
			score = 85.0
			needsReview = true
		} else if partialName && setMatched {
			score = 70.0
			needsReview = true
		} else if collectorMatched {
			score = 65.0
			needsReview = true
		} else {
			continue // No meaningful match
		}

		if info.Confidence < 0.70 {
			needsReview = true
			if score > 80.0 {
				score = 80.0
			}
		}

		candidates = append(candidates, scoredMatch{
			card:        card,
			confidence:  score,
			needsReview: needsReview,
		})
	}

	// Sort by confidence descending
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].confidence > candidates[j].confidence
	})

	// Mark ties within 5 points for review
	if len(candidates) >= 2 && (candidates[0].confidence-candidates[1].confidence) <= 5.0 {
		candidates[0].needsReview = true
		candidates[1].needsReview = true
	}

	maxTop := 5
	if len(candidates) > maxTop {
		candidates = candidates[:maxTop]
	}

	var topMatches []CardMatch
	for i := range candidates {
		c := candidates[i]
		cardCopy := c.card
		topMatches = append(topMatches, CardMatch{
			Card:        &cardCopy,
			Confidence:  c.confidence,
			NeedsReview: c.needsReview,
			OCRScore: &ConfidenceScore{
				Method:   "claude_vision",
				Score:    c.confidence,
				CardName: c.card.Name,
				CardID:   c.card.ID,
				RawText:  formatClaudeText(info),
			},
		})
	}

	// If no match was found in the provided catalog slice, create a synthetic card match
	// so the user still benefits from Claude's extraction details
	if len(topMatches) == 0 && info.Name != "" {
		synthCard := models.Card{
			ID:              "",
			Name:            info.Name,
			Set:             info.Set,
			SetCode:         info.SetCode,
			CollectorNumber: info.CollectorNumber,
			Language:        info.Language,
			Game:            info.Game,
			Variant:         info.Variant,
		}
		conf := info.Confidence * 100
		if conf <= 0 {
			conf = 85.0
		}
		topMatches = append(topMatches, CardMatch{
			Card:        &synthCard,
			Confidence:  conf,
			NeedsReview: true,
			OCRScore: &ConfidenceScore{
				Method:   "claude_vision",
				Score:    conf,
				CardName: synthCard.Name,
				CardID:   "",
				RawText:  formatClaudeText(info),
			},
		})
	}

	return topMatches
}

func normalizeCollectorNum(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "#", "")
	s = strings.ReplaceAll(s, "no.", "")
	return s
}

func extractNumerator(s string) string {
	if idx := strings.Index(s, "/"); idx != -1 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}
