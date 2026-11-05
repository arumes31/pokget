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
	"image"
	"regexp"
	"sort"
	"strconv"
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

// MultiImageClaudeDetector is optionally implemented by detectors that can
// analyze magnified crops of the same card in one request. When a detector does
// not implement it the pipeline falls back to the single full-card image.
type MultiImageClaudeDetector interface {
	DetectImages(ctx context.Context, images [][]byte, gameHint, langHint string) (*claudedetect.DetectedCardInfo, error)
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
		langHint = claudeLanguageHint(request.Scope.Language)
	}

	// The full card plus magnified title and bottom-information strips. The strips
	// are what make kana (ピ vs ビ) and collector numbers reliable.
	images, err := claudeImageVariants(ctx, request.Image)
	if err != nil {
		finish(err)
		result.Metrics.Stages = append(result.Metrics.Stages, DetectionStageMetrics{
			Name: "image_preparation", Error: err,
		})
		return nil, err
	}
	full := images[0]
	var info *claudedetect.DetectedCardInfo
	if multi, ok := p.Claude.(MultiImageClaudeDetector); ok && len(images) > 1 {
		info, err = multi.DetectImages(ctx, images, gameHint, langHint)
		if err != nil && ctx.Err() == nil {
			// The crops are an enhancement: never let an oversized or rejected
			// enhancement payload turn a readable card into a failed scan.
			ScanLogger(ctx).Warn("Claude magnified crops rejected; retrying the full card only")
			info, err = p.Claude.Detect(ctx, full, gameHint, langHint)
		}
	} else {
		info, err = p.Claude.Detect(ctx, full, gameHint, langHint)
	}
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
	p.applyFingerprintTieBreak(result, request.Cards, request.Image, normalizeCollectorNum(info.CollectorNumber) != "")
	result.Metrics.TotalTime = time.Since(totalStart)

	// A hint can be wrong. Do not silently answer in the selected language/game:
	// report the conflict so the user can rescan with the right scope.
	if conflict := detectionScopeConflict(request.Scope, info); conflict != nil {
		result.Conflict = conflict
		if conflict.Field == "game" {
			result.Status = DetectionStatusGameConflict
		} else {
			result.Status = DetectionStatusLanguageConflict
		}
		for index := range result.TopMatches {
			result.TopMatches[index].NeedsReview = true
		}
	} else if len(result.TopMatches) > 0 {
		top := result.TopMatches[0]
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
		"images", len(images),
		"game_hint", gameHint,
		"language_hint", langHint,
		"cache_read_tokens", info.CacheReadTokens,
		"cache_created_tokens", info.CacheCreationTokens,
		"top_match", result.BestMatchName(),
		"confidence", result.BestMatchConfidence())

	return result, nil
}

// applyFingerprintTieBreak resolves catalog rows whose readable text is
// identical (alt art, foil, reprint) with the stored perceptual hash. It runs
// even when the text scorer was confident, because the base row can win on set
// evidence while the photo shows another printing. Only a close hash that is
// clearly ahead of the runner-up is accepted; otherwise the printing becomes a
// review choice instead of a silent guess. When the detector read no collector
// number, every printing of that name competes on artwork, which is the only
// evidence left for cards printed without a number.
func (p *DetectionPipeline) applyFingerprintTieBreak(result *DetectionResult, cards []models.Card, imageBytes []byte, collectorKnown bool) {
	if p == nil || p.Fingerprint == nil || result == nil || len(result.TopMatches) == 0 {
		return
	}
	top := result.TopMatches[0]
	if top.Card == nil || top.Card.ID == "" {
		return
	}
	groupKey := func(card models.Card) string {
		if collectorKnown {
			return fingerprintGroupKey(card)
		}
		return normalizeMatchText(card.Name) + "\x00" + normalizeMatchText(card.Language)
	}
	key := groupKey(*top.Card)
	group := make([]models.Card, 0, 8)
	distinct := make(map[string]struct{}, 4)
	for _, card := range cards {
		if card.Phash == nil || groupKey(card) != key {
			continue
		}
		group = append(group, card)
		distinct[card.ID] = struct{}{}
	}
	if len(group) == 0 {
		return
	}
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return
	}
	hash, err := p.Fingerprint.CalculateHash(decoded)
	if err != nil {
		return
	}
	if len(group) < 2 || len(distinct) < 2 {
		// A single candidate still has to look like the photo. A confident text
		// match on the wrong row (a misread digit such as 006 vs 008) lands far
		// from the stored artwork hash and must become a review choice.
		if top.Card.Phash != nil &&
			hammingDistance(uint64(hash), uint64(*top.Card.Phash)) > p.Fingerprint.PhashPotential { // #nosec G115 - comparing hash bits, not a numeric value
			result.TopMatches[0].NeedsReview = true
		}
		return
	}
	// Printings with identical artwork cannot be told apart by the hash. Some
	// catalogs duplicate one card across set variants (Altered CORE/COREKS) with
	// the same art and number; the printed set symbol already decided, so the
	// artwork must not turn an exact text match into a review.
	identicalArtwork := true
	for index := 1; index < len(group); index++ {
		if *group[index].Phash != *group[0].Phash {
			identicalArtwork = false
			break
		}
	}
	if identicalArtwork {
		return
	}
	type rankedCard struct {
		card     models.Card
		distance int
	}
	ranked := make([]rankedCard, 0, len(group))
	for _, card := range group {
		ranked = append(ranked, rankedCard{card: card, distance: hammingDistance(uint64(hash), uint64(*card.Phash))}) // #nosec G115 - comparing hash bits, not a numeric value
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].distance < ranked[j].distance })
	if ranked[0].distance <= p.Fingerprint.PhashHighConf &&
		(len(ranked) == 1 || ranked[1].distance-ranked[0].distance >= 3) {
		winner := ranked[0].card
		result.TopMatches[0].Card = &winner
		// Without a printed number the artwork is the only printing evidence, so
		// the score says "name + artwork", not "name + number + set".
		result.TopMatches[0].Confidence = 99
		if !collectorKnown {
			result.TopMatches[0].Confidence = 92
		}
		result.TopMatches[0].NeedsReview = false
		return
	}
	result.TopMatches[0].NeedsReview = true
}

// fingerprintGroupKey groups printings that share every readable field: name,
// number and printed language. The set is deliberately excluded, because alt
// art and reprint rows often carry a different product set while the card face
// still reads the same. Variant markers in the number ("202s", "026a/221") are
// stripped too: the detector may have dropped the marker, and the artwork hash
// is exactly what can tell those printings apart.
func fingerprintGroupKey(card models.Card) string {
	return normalizeMatchText(card.Name) + "\x00" +
		collectorVariantBase(card.CollectorNumber) + "\x00" +
		normalizeMatchText(card.Language)
}

// variantEvidence compares the printed finish the detector reported with the
// catalog variant. "Normal" rows only conflict with a specific finish
// (Rainbow/Cold/Gold/Parallel/...); a generic "Holo"/"Foil" reading is noise on
// ordinary cards and must not send every scan to review. A generic reading also
// cannot identify one specific foil type.
func variantEvidence(detected, catalog string) (matched, conflict bool) {
	if detected == "" || catalog == "" {
		return false, false
	}
	// The model says "Standard" / "Regular" for a plain card; the catalogs say
	// "Normal". Treat those as the same finish, otherwise a plain row is sent to
	// review merely because a foil sibling exists.
	plain := func(value string) bool {
		switch value {
		case "standard", "regular", "nonfoil", "non foil", "plain", "normal", "base":
			return true
		}
		return false
	}
	if plain(detected) {
		detected = "normal"
	}
	if plain(catalog) {
		catalog = "normal"
	}
	if detected == catalog {
		return true, false
	}
	// Different catalogs name the same finish differently ("Parallel Foil" vs
	// "Parallel (P) print"). A shared specific marker is the same finish family;
	// a generic "Foil" still cannot identify which foil.
	for _, marker := range []string{"rainbow", "cold", "gold", "parallel", "alternate", "alt art", "reverse", "etched", "showcase", "borderless", "extended"} {
		if strings.Contains(detected, marker) && strings.Contains(catalog, marker) {
			return true, false
		}
	}
	specific := func(value string) bool {
		for _, marker := range []string{"rainbow", "cold", "gold", "parallel", "alternate", "alt art", "reverse", "etched", "showcase", "borderless", "extended"} {
			if strings.Contains(value, marker) {
				return true
			}
		}
		return false
	}
	generic := func(value string) bool {
		return value == "foil" || value == "holo" || value == "holofoil" || value == "foil holo"
	}
	if catalog == "normal" {
		if specific(detected) {
			return false, true
		}
		return false, false
	}
	if strings.Contains(detected, catalog) {
		return true, false
	}
	if strings.Contains(catalog, detected) && len(detected) >= 5 && len(catalog)-len(detected) <= 4 {
		return true, false
	}
	if generic(detected) {
		return false, false
	}
	return false, true
}

// detectionScopeConflict compares the printed language/game reported by the
// detector with the collector's selection. The prompt asks the model to report
// what it actually sees, so a mismatch here is evidence of a wrong hint rather
// than a reading error.
func detectionScopeConflict(scope ScanScope, info *claudedetect.DetectedCardInfo) *DetectionConflict {
	if info == nil {
		return nil
	}
	if scope.Language.Valid() && scope.Language != models.LanguageAny {
		if detected, err := models.ParseLanguage(info.Language); err == nil {
			if detected.Valid() && detected != models.LanguageAny && detected != scope.Language {
				return &DetectionConflict{
					Field:    "language",
					Selected: scope.Language.ISO639Code(),
					Detected: detected.ISO639Code(),
				}
			}
		}
	}
	if scope.TCG != models.TCGUnknown {
		if detected, err := models.ParseTCG(info.Game); err == nil {
			if detected != models.TCGUnknown && detected != scope.TCG {
				return &DetectionConflict{
					Field:    "game",
					Selected: string(scope.TCG),
					Detected: string(detected),
				}
			}
		}
	}
	return nil
}

// claudeLanguageHint names the selected language instead of passing a bare ISO
// code, so the model cannot mistake "ja" for a set code or ignore it.
func claudeLanguageHint(language models.Language) string {
	if language == models.LanguageAny || !language.Valid() {
		return ""
	}
	names := map[models.Language]string{
		models.LanguageEnglish:            "English",
		models.LanguageJapanese:           "Japanese",
		models.LanguageGerman:             "German",
		models.LanguageFrench:             "French",
		models.LanguageSpanish:            "Spanish",
		models.LanguageItalian:            "Italian",
		models.LanguageChineseSimplified:  "Simplified Chinese",
		models.LanguageChineseTraditional: "Traditional Chinese",
		models.LanguageKorean:             "Korean",
	}
	name := names[language]
	if name == "" {
		return string(language)
	}
	return name + " (" + string(language) + ", " + language.ISO639Code() + ")"
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
		card            models.Card
		confidence      float64
		needsReview     bool
		setExact        bool
		collectorExact  bool
		variantMatched  bool
		variantConflict bool
	}

	var candidates []scoredMatch
	normClaudeName := normalizeMatchText(info.Name)
	compactClaudeName := compactMatchText(normClaudeName)
	foldedClaudeName := foldVoicing(normClaudeName)
	claudeCollector := normalizeCollectorNum(info.CollectorNumber)
	claudeCollectorBase := collectorBaseKey(claudeCollector)
	claudeCollectorCanonical := collectorCanonicalKey(info.CollectorNumber)
	claudeSet := normalizeMatchText(info.Set)
	claudeSetCode := normalizeMatchText(info.SetCode)
	claudeVariant := normalizeMatchText(info.Variant)

	for _, card := range cards {
		if !card.IsCatalogActive() {
			continue
		}

		cardNormName := normalizeMatchText(card.Name)
		cardCollector := normalizeCollectorNum(card.CollectorNumber)
		cardCollectorBase := collectorBaseKey(cardCollector)
		cardCollectorCanonical := collectorCanonicalKey(card.CollectorNumber)
		cardSet := normalizeMatchText(card.Set)
		cardSetCode := normalizeMatchText(card.SetCode)
		variantMatched, variantConflict := variantEvidence(claudeVariant, normalizeMatchText(card.Variant))

		// Name matching (check primary and localized names). Whitespace is not
		// meaning: Japanese card names are printed without the spaces the
		// catalog uses ("かがやくヒスイオオニューラ" vs "かがやくヒスイ オオニューラ").
		nameMatched := cardNormName == normClaudeName ||
			(compactClaudeName != "" && compactMatchText(cardNormName) == compactClaudeName)
		if !nameMatched {
			for _, loc := range card.LocalizedNames {
				normalizedLocalized := normalizeMatchText(loc)
				if normalizedLocalized == normClaudeName ||
					(compactClaudeName != "" && compactMatchText(normalizedLocalized) == compactClaudeName) {
					nameMatched = true
					break
				}
			}
		}

		// Voicing-folded name matching. A single dakuten/handakuten is the most
		// common vision error in Japanese names (ピジョット read as ビジョット),
		// so the folded form still finds the card family. It is evidence for
		// review, never proof, because two real cards can differ by that mark.
		foldedNameMatched := false
		if !nameMatched && foldedClaudeName != "" && foldVoicing(cardNormName) == foldedClaudeName {
			foldedNameMatched = true
		}
		if !nameMatched && !foldedNameMatched {
			for _, loc := range card.LocalizedNames {
				if foldedClaudeName != "" && foldVoicing(normalizeMatchText(loc)) == foldedClaudeName {
					foldedNameMatched = true
					break
				}
			}
		}

		// Partial / substring name matching
		partialName := false
		if !nameMatched && compactClaudeName != "" {
			compactCardName := compactMatchText(cardNormName)
			if compactCardName != "" {
				if strings.Contains(compactCardName, compactClaudeName) || strings.Contains(compactClaudeName, compactCardName) {
					partialName = true
				}
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
				// The catalog stores the bare collector number ("1") while the
				// card prints a fraction ("1/102"). The numerator is the card
				// number; the denominator is set-size information the catalog
				// does not carry, so it must not weaken the match.
				cNum1 := extractNumerator(claudeCollector)
				cNum2 := extractNumerator(cardCollector)
				kNum1 := collectorNumberKey(cNum1)
				kNum2 := collectorNumberKey(cNum2)
				if (kNum1 != "" && kNum1 == kNum2) ||
					(collectorCanonicalKey(cNum1) != "" && collectorCanonicalKey(cNum1) == collectorCanonicalKey(cNum2)) {
					collectorMatched = true
				}
			} else {
				// Neither has fraction: compare keys (strips leading zeroes)
				if (k1 != "" && k1 == k2) || claudeCollector == cardCollector {
					collectorMatched = true
				} else if claudeCollectorCanonical != "" && claudeCollectorCanonical == cardCollectorCanonical {
					collectorMatched = true
				} else if claudeCollectorCanonical != "" && strings.Contains(cardCollectorCanonical, "-") &&
					collectorCanonicalKey(strings.SplitN(cardCollectorCanonical, "-", 2)[0]) == claudeCollectorCanonical {
					// Some catalogs append edition/finish tokens to the printed
					// number ("006-cage_of_sidrak-b-f" for a card that prints
					// "006"). The leading token is the number; the rest is
					// printing metadata, so this is partial evidence.
					partialCollector = true
				} else if claudeCollectorBase != "" && claudeCollectorBase == cardCollectorBase {
					// Magic prints the same number with a variant suffix
					// (254p / 254s): the base number still identifies the card,
					// but not the printing, so it is partial evidence. This only
					// holds when the detector read the base form; a suffix the
					// detector did read but the catalog row lacks is a different
					// printing and must not match the base row.
					if !hasCollectorVariantSuffix(claudeCollector) && hasCollectorVariantSuffix(cardCollector) {
						partialCollector = true
					}
				}
			}
		}

		// Set / SetCode matching. Exact equality is the only form strong enough
		// to identify a card without name evidence: generic set names are
		// substrings of many products ("Commander Legends" is inside
		// "Commander Legends: Battle for Baldur's Gate"), so substring matches
		// must not lift an unrelated card over the real one.
		setMatchedExact := false
		if claudeSetCode != "" && cardSetCode != "" && claudeSetCode == cardSetCode {
			setMatchedExact = true
		} else if claudeSet != "" && cardSet != "" && claudeSet == cardSet {
			setMatchedExact = true
		}
		setMatched := setMatchedExact
		if !setMatched && claudeSet != "" && cardSet != "" {
			if strings.Contains(cardSet, claudeSet) || strings.Contains(claudeSet, cardSet) {
				setMatched = true
			}
		}

		// Scoring logic
		var score float64
		needsReview := false

		if nameMatched && collectorMatched {
			// Exact name + exact collector number = definitive match!
			score = 98.0
			if setMatchedExact {
				score = 99.0
			}
			needsReview = false
		} else if nameMatched && partialCollector {
			if setMatchedExact {
				score = 94.0
				needsReview = false
			} else {
				score = 86.0
				needsReview = true
			}
		} else if nameMatched && setMatchedExact {
			// Name and an exactly matching set, but no collector number: the
			// reprint is still ambiguous unless the name exists only once.
			score = 92.0
			needsReview = true
		} else if nameMatched && setMatched {
			score = 90.0
			needsReview = true
		} else if foldedNameMatched && collectorMatched && setMatchedExact {
			// The printed number and set identify the printing; the name differs
			// only by a voicing mark (ピ/ビ), which is the most common Japanese
			// vision error. Number + set are decisive, so no review is needed.
			score = 96.0
			needsReview = false
		} else if partialName && collectorMatched && setMatchedExact {
			// The printed number carries the variant marker and matches a row
			// whose name extends the detected one ("Body Rune R04c",
			// "… Parallel Foil", "… SRP"). The number identifies the printing;
			// the extra name tokens are catalog labels, not a different card.
			score = 95.0
			needsReview = false
		} else if collectorMatched && setMatchedExact {
			// Number and set agree but the name gives no evidence (a different
			// language, or an unclear title): offer it for review, never as a
			// confirmed match.
			score = 88.0
			needsReview = true
		} else if foldedNameMatched && collectorMatched {
			score = 84.0
			needsReview = true
		} else if nameMatched {
			score = 75.0
			needsReview = true
		} else if foldedNameMatched && setMatched {
			score = 82.0
			needsReview = true
		} else if partialName && collectorMatched {
			score = 85.0
			needsReview = true
		} else if partialName && setMatched {
			score = 70.0
			needsReview = true
		} else if foldedNameMatched {
			score = 74.0
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
		// Variant conflicts are resolved where the competing printings are known
		// (the tie pass). A single catalog row cannot represent a finish the model
		// claims, so it must not block the match on its own: Pokémon and hololive
		// store most rows as "Normal" even for holo/parallel cards.

		// A specific finish the catalog records is conflicting evidence when the
		// detector read the plain card: the plain printing is not in the catalog.
		// The reverse (a specific read on a Normal row) stays suppressed because
		// several sources store every row as Normal even for holo cards.
		if variantConflict && normalizeMatchText(card.Variant) != "normal" {
			needsReview = true
		}

		candidates = append(candidates, scoredMatch{
			card:            card,
			confidence:      score,
			needsReview:     needsReview,
			setExact:        setMatchedExact,
			collectorExact:  collectorMatched,
			variantMatched:  variantMatched,
			variantConflict: variantConflict,
		})
	}

	// When the scope spans languages (Auto), the language printed on the card is
	// the only field that separates identical printings (Base Set Pikachu #58
	// exists in de/en/fr with the same name and number). Keep the model's
	// language and fall back to all candidates when it matches none.
	if detectedLanguage, err := models.ParseLanguage(info.Language); err == nil && detectedLanguage != models.LanguageAny {
		matching := make([]scoredMatch, 0, len(candidates))
		for _, candidate := range candidates {
			if detectedLanguage.Matches(candidate.card.Language) {
				matching = append(matching, candidate)
			}
		}
		if len(matching) > 0 {
			candidates = matching
		}
	}

	// Sort by confidence descending. Ties are broken deterministically, and the
	// plain printing wins over an alt-art row: One Piece stores the same name and
	// collector number on the base card and on every `_pN`/`_rN` reprint, so
	// without this the awarded row would depend on database heap order.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].confidence != candidates[j].confidence {
			return candidates[i].confidence > candidates[j].confidence
		}
		// Equal text evidence: the printed finish decides which printing the
		// collector is holding.
		if candidates[i].variantMatched != candidates[j].variantMatched {
			return candidates[i].variantMatched
		}
		baseI, baseJ := isBasePrinting(candidates[i].card), isBasePrinting(candidates[j].card)
		if baseI != baseJ {
			return baseI
		}
		return candidates[i].card.ID < candidates[j].card.ID
	})

	// A unique name in this scan scope is enough to identify the card when the
	// number was unreadable: no other candidate shares the name, so the name-only
	// bucket is a decision, not a guess. A name that only contains the detected
	// name ("Blue Slushy Cookie SEC", "Sadistic Strategy Iconic Art") is a
	// different printing, so those candidates count as related too.
	if len(candidates) > 0 {
		top := &candidates[0]
		if (top.confidence == 75.0 || top.confidence == 92.0) && !top.collectorExact {
			topName := compactMatchText(normalizeMatchText(top.card.Name))
			related := 0
			for index := range cards {
				otherName := compactMatchText(normalizeMatchText(cards[index].Name))
				if otherName == topName || strings.Contains(otherName, topName) || strings.Contains(topName, otherName) {
					related++
				}
			}
			if related == 1 {
				top.confidence = 85.0
				top.needsReview = false
			}
		}
	}

	// Variant markers live in the catalog number or in a name suffix ("202s",
	// "026a/221", "ESD01-015+", "… SEC"). A detector that reads the base form
	// must not silently pick the base printing, even when name and set match
	// exactly: the photographed card can be the variant.
	if len(candidates) > 0 && claudeCollectorCanonical != "" {
		top := &candidates[0]
		topName := compactMatchText(normalizeMatchText(top.card.Name))
		topCollector := collectorCanonicalKey(top.card.CollectorNumber)
		for index := 1; index < len(candidates); index++ {
			other := &candidates[index]
			otherFull := collectorCanonicalKey(other.card.CollectorNumber)
			if otherFull != "" &&
				normalizeCollectorNum(other.card.CollectorNumber) != normalizeCollectorNum(top.card.CollectorNumber) &&
				collectorVariantBase(other.card.CollectorNumber) == claudeCollectorCanonical {
				top.needsReview = true
				other.needsReview = true
				break
			}
			// A reprint whose catalog number wraps the same digits in letters
			// ("cr124sb" for a card that prints 124) belongs to a different
			// product. The printed set symbol decides; without it the scan must
			// ask instead of trusting the model's set guess.
			if other.card.Set != top.card.Set && otherFull != "" && topCollector != "" &&
				digitsOnly(other.card.CollectorNumber) == digitsOnly(top.card.CollectorNumber) &&
				digitsOnly(top.card.CollectorNumber) != "" &&
				hasLetter(other.card.CollectorNumber) && !hasLetter(top.card.CollectorNumber) {
				top.needsReview = true
				other.needsReview = true
				break
			}
			// A same-number row whose name extends the detected one is a variant
			// printing only inside the same product: "Rocket's Mewtwo" #8 in
			// another set merely contains the detected name "Mew" and is a
			// different card, not a finish choice.
			if topCollector != "" && otherFull == topCollector && setIdentity(other.card) == setIdentity(top.card) {
				otherName := compactMatchText(normalizeMatchText(other.card.Name))
				if otherName != topName && (strings.Contains(otherName, topName) || strings.Contains(topName, otherName)) {
					top.needsReview = true
					other.needsReview = true
					break
				}
			}
		}
	}

	// Mark ties within 5 points for review. A tie between different cards is
	// ambiguous unless the top match carries decisive evidence (name + exact
	// number + exact set). Rows of the same card that differ in variant are an
	// alt-art/reprint choice and need the user, because the price differs.
	if len(candidates) >= 2 {
		top := &candidates[0]
		topKey := logicalCardKey(top.card)
		topVariant := normalizeMatchText(top.card.Variant)
		for i := 1; i < len(candidates); i++ {
			if top.confidence-candidates[i].confidence > 5.0 {
				break
			}
			if logicalCardKey(candidates[i].card) != topKey {
				if top.setExact && top.collectorExact {
					continue
				}
				top.needsReview = true
				candidates[i].needsReview = true
				break
			}
			// Same name+number+set+language: two catalog rows can still be
			// different printings (alt art, foil) whose readable text is
			// identical. A printed finish that matches exactly one of them
			// decides; otherwise the scan must ask.
			otherVariant := normalizeMatchText(candidates[i].card.Variant)
			if otherVariant != topVariant || candidates[i].card.SourceCardID != top.card.SourceCardID {
				if top.variantMatched && !candidates[i].variantMatched {
					continue
				}
				top.needsReview = true
				candidates[i].needsReview = true
				break
			}
		}
	}

	// Name + an exactly matching set is enough when that set contains the name
	// only once: the unread collector number cannot belong to another row in
	// the same set because there is no other row. Reprints in other sets are
	// different printings, and the printed set was read exactly. Name-suffix
	// variants ("... Parallel Foil", "... SRP", "Body Rune R04c") are different
	// printings even though the detected base name matches only one of them.
	if len(candidates) > 0 {
		top := &candidates[0]
		if top.confidence == 92.0 && top.setExact && top.needsReview {
			topName := compactMatchText(normalizeMatchText(top.card.Name))
			topSet := setIdentity(top.card)
			relatedInSet := 0
			for index := range cards {
				if setIdentity(cards[index]) != topSet {
					continue
				}
				otherName := compactMatchText(normalizeMatchText(cards[index].Name))
				if otherName == topName || strings.Contains(otherName, topName) || strings.Contains(topName, otherName) {
					relatedInSet++
				}
			}
			if relatedInSet == 1 {
				top.needsReview = false
			}
		}
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
	// WIXOSS stores the language tag in the number ("WXDi-P12-013[EN]") while the
	// card prints only the code. Remove bracketed metadata before comparing.
	s = collectorBracketPattern.ReplaceAllString(s, "")
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

var (
	collectorVariantSuffixPattern = regexp.MustCompile(`^([a-z]{0,3})([0-9]{1,4})[a-z]$`)
	collectorVariantMarkerPattern = regexp.MustCompile(`([0-9])([a-z])(\[|/|$)`)
	altArtSourceIDPattern         = regexp.MustCompile(`(?i)_[pr][0-9]+$`)
	collectorSeparatorPattern     = regexp.MustCompile(`[^a-z0-9]+`)
	collectorBracketPattern       = regexp.MustCompile(`\[[^\]]*\]`)
)

// collectorVariantBase strips the markers catalogs use for alternate printings
// ("202s", "026a/221", "ESD01-015+", "…044P[EN]") so the base form can be
// compared with what the detector read.
func collectorVariantBase(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimSuffix(trimmed, "+")
	trimmed = collectorVariantMarkerPattern.ReplaceAllString(trimmed, "$1$3")
	return collectorCanonicalKey(trimmed)
}

// hasCollectorVariantSuffix reports whether the number carries a printing
// marker (254p, R04c, 026a/221, ESD01-015+, …044P[EN]) beyond the bare card
// number. It decides whether a base-number match is partial evidence or a
// different printing.
func hasCollectorVariantSuffix(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(trimmed, "+") {
		return true
	}
	return collectorVariantBase(value) != collectorCanonicalKey(value)
}

func digitsOnly(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func hasLetter(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// collectorCanonicalKey makes the printed number comparable across the shapes
// the model and the catalog use: `TAEV-EN027`, `TAEV EN27`, `taev_en027` and
// `TAEV-EN27` all reduce to the same key. Yu-Gi-Oh and One Piece numbers carry
// a set prefix, so the old `[a-z]{0,3}[0-9]+` parser returned no key at all for
// them and only an exact byte match counted.
func collectorCanonicalKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = collectorBracketPattern.ReplaceAllString(value, "")
	value = strings.TrimPrefix(value, "#")
	value = strings.ReplaceAll(value, "no.", "")
	value = collectorSeparatorPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "-")
	last := parts[len(parts)-1]
	digitStart := len(last)
	for digitStart > 0 && last[digitStart-1] >= '0' && last[digitStart-1] <= '9' {
		digitStart--
	}
	if digitStart < len(last) {
		number, err := strconv.Atoi(last[digitStart:])
		if err == nil {
			parts[len(parts)-1] = last[:digitStart] + strconv.Itoa(number)
		}
	}
	return strings.Join(parts, "-")
}

// collectorBaseKey is the collector number without the single-letter variant
// suffix Magic uses for alternate printings (`254p`, `254s`). It still keeps the
// numeric identity of the card, so a scan that reads only "254" can match it as
// partial evidence instead of finding no candidate at all.
func collectorBaseKey(value string) string {
	if key := collectorNumberKey(value); key != "" {
		return key
	}
	match := collectorVariantSuffixPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if match == nil {
		return ""
	}
	number, err := strconv.Atoi(match[2])
	if err != nil {
		return ""
	}
	return match[1] + strconv.Itoa(number)
}

// isBasePrinting prefers the plain catalog row over an alt-art/reprint row.
func isBasePrinting(card models.Card) bool {
	return card.SourceCardID == "" || !altArtSourceIDPattern.MatchString(card.SourceCardID)
}

// logicalCardKey identifies one card across its printings: reprints share name
// and collector number but differ in artwork, price and catalog row.
func logicalCardKey(card models.Card) string {
	return normalizeMatchText(card.Name) + "\x00" +
		normalizeCollectorNum(card.CollectorNumber) + "\x00" +
		setIdentity(card) + "\x00" +
		normalizeMatchText(card.Language)
}

func setIdentity(card models.Card) string {
	if code := normalizeMatchText(card.SetCode); code != "" {
		return code
	}
	return normalizeMatchText(card.Set)
}

// compactMatchText removes the spaces normalizeMatchText leaves behind.
func compactMatchText(value string) string {
	return strings.ReplaceAll(value, " ", "")
}
