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
	"errors"
	"image"
	"testing"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
)

type mockClaudeDetector struct {
	info *claudedetect.DetectedCardInfo
	err  error
}

func (m *mockClaudeDetector) Detect(ctx context.Context, imageBytes []byte, gameHint, langHint string) (*claudedetect.DetectedCardInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.info, nil
}

func (m *mockClaudeDetector) Model() string {
	return "claude-sonnet-5-5"
}

func (m *mockClaudeDetector) PromptCachingEnabled() bool {
	return true
}

func TestClaudeDetection_ExactMatch(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:                "pokemon",
			Name:                "Rayquaza",
			CollectorNumber:     "16/110",
			Set:                 "EX Holon Phantoms",
			SetCode:             "HP",
			Language:            "deu",
			Variant:             "Holo Delta Species",
			Confidence:          0.98,
			CacheReadTokens:     1774,
			CacheCreationTokens: 0,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{
			ID:              "rayquaza-hp-16",
			Name:            "Rayquaza",
			CollectorNumber: "16/110",
			Set:             "EX Holon Phantoms",
			SetCode:         "HP",
			Language:        "deu",
			Game:            "pokemon",
		},
		{
			ID:              "pikachu-hp-78",
			Name:            "Pikachu",
			CollectorNumber: "78/110",
			Set:             "EX Holon Phantoms",
			Game:            "pokemon",
		},
	}

	req := DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
		Scope: ScanScope{
			TCG:      models.TCGPokemon,
			Language: models.LanguageGerman,
		},
	}

	result, err := pipeline.DetectScoped(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != DetectionStatusMatched {
		t.Errorf("expected DetectionStatusMatched, got %s", result.Status)
	}
	if result.BestMatchID() != "rayquaza-hp-16" {
		t.Errorf("expected rayquaza-hp-16, got %s", result.BestMatchID())
	}
	if result.BestMatchConfidence() < 95.0 {
		t.Errorf("expected high confidence, got %f", result.BestMatchConfidence())
	}
	if result.BestMatchNeedsReview() {
		t.Errorf("expected BestMatchNeedsReview false")
	}
}

func TestClaudeDetection_UncatalogedSyntheticCard(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "one_piece",
			Name:            "Monkey.D.Luffy",
			CollectorNumber: "OP01-001",
			Set:             "Romance Dawn",
			Language:        "eng",
			Variant:         "Leader Parallel",
			Confidence:      0.95,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	// Empty catalog: Claude detected card not currently in catalog slice
	cards := []models.Card{}

	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
	}, false, time.Now())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != DetectionStatusNeedsReview {
		t.Errorf("expected NeedsReview, got %s", result.Status)
	}
	if result.BestMatchID() != "" {
		t.Errorf("expected empty BestMatchID for synthetic match, got %s", result.BestMatchID())
	}
	if result.BestMatchName() != "Monkey.D.Luffy" {
		t.Errorf("expected Monkey.D.Luffy, got %s", result.BestMatchName())
	}
	if result.BestMatchCard().CollectorNumber != "OP01-001" {
		t.Errorf("expected OP01-001, got %s", result.BestMatchCard().CollectorNumber)
	}
}

func TestClaudeDetection_Synthetic_NoSecondaryFallback(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "one_piece",
			Name:            "Unknown Secret Card",
			CollectorNumber: "OP99-999",
			Confidence:      0.85,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{ID: "card-pika", Name: "Pikachu", CollectorNumber: "25"},
	}

	result, err := pipeline.detect(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
	}, false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Synthetic match from Claude is used directly without secondary fallback
	if result.BestMatchID() != "" {
		t.Errorf("expected empty BestMatchID for synthetic match, got %s", result.BestMatchID())
	}
	if result.BestMatchName() != "Unknown Secret Card" {
		t.Errorf("expected Unknown Secret Card, got %s", result.BestMatchName())
	}
	if result.Status != DetectionStatusNeedsReview {
		t.Errorf("expected NeedsReview, got %s", result.Status)
	}
}

func TestClaudeDetection_Error_NoSecondaryFallback(t *testing.T) {
	mock := &mockClaudeDetector{
		err: errors.New("claude rate limit or timeout"),
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{ID: "card-pika", Name: "Pikachu", CollectorNumber: "25"},
	}

	result, err := pipeline.detect(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
	}, false)

	if err == nil {
		t.Fatal("expected error from Claude detection failure without secondary fallback")
	}
	if result == nil || result.Status != DetectionStatusFailed {
		t.Errorf("expected DetectionStatusFailed, got %+v", result)
	}
}

func TestClaudeDetection_MatchesLocalizedPrintedName(t *testing.T) {
	// Magic keeps the English name in `name`; the Japanese card prints the
	// localized name. Without LocalizedNames the detector read this card
	// correctly and matched a different card by set+number instead.
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "magic",
			Name:            "イモエン、神秘のトリックスター",
			CollectorNumber: "385",
			Set:             "統率者レジェンズ：バルダーズ・ゲートの戦い",
			Language:        "ja",
			Confidence:      0.93,
		},
	}

	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{
			ID:              "clb-385",
			Name:            "Imoen, Mystic Trickster",
			LocalizedNames:  []string{"イモエン、神秘のトリックスター"},
			CollectorNumber: "385",
			Set:             "Commander Legends: Battle for Baldur's Gate",
			SetCode:         "clb",
			Language:        "ja",
			Game:            "magic",
		},
		{
			ID:              "cmr-385",
			Name:            "Spirit Mantle",
			CollectorNumber: "385",
			Set:             "Commander Legends",
			SetCode:         "cmr",
			Language:        "ja",
			Game:            "magic",
		},
	}

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGMagic, Language: models.LanguageJapanese},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "clb-385" {
		t.Fatalf("expected localized name to win clb-385, got %q", result.BestMatchID())
	}
	if result.BestMatchNeedsReview() {
		t.Errorf("exact localized name + number must not need review")
	}
}

func TestClaudeDetection_VariantSuffixCollectorIsPartialEvidence(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "magic",
		Name:            "Mirrex",
		CollectorNumber: "254",
		Set:             "Phyrexia: All Will Be One",
		Language:        "en",
		Confidence:      0.95,
	}
	cards := []models.Card{
		{
			ID:              "pone-254p",
			Name:            "Mirrex",
			CollectorNumber: "254p",
			Set:             "Phyrexia: All Will Be One",
			SetCode:         "pone",
			Language:        "en",
			Game:            "magic",
			SourceCardID:    "254p",
		},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 1 {
		t.Fatalf("expected one candidate, got %d", len(matches))
	}
	if matches[0].Confidence != 94.0 {
		t.Errorf("expected partial-variant confidence 94, got %f", matches[0].Confidence)
	}
	if matches[0].NeedsReview {
		t.Errorf("name + set + base number is a confident match")
	}
}

func TestClaudeDetection_DuplicatePrintingsRequireReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "one_piece",
		Name:            "Boa Hancock",
		CollectorNumber: "ST17-004",
		Set:             "ST-17",
		Language:        "en",
		Confidence:      0.96,
	}
	cards := []models.Card{
		{
			ID:              "card-alt",
			Name:            "Boa Hancock",
			CollectorNumber: "ST17-004",
			Set:             "ST-17",
			SetCode:         "ST-17",
			Language:        "en",
			Game:            "one_piece",
			SourceCardID:    "ST17-004_p1",
		},
		{
			ID:              "card-base",
			Name:            "Boa Hancock",
			CollectorNumber: "ST17-004",
			Set:             "ST-17",
			SetCode:         "ST-17",
			Language:        "en",
			Game:            "one_piece",
			SourceCardID:    "ST17-004",
		},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if matches[0].Card.ID != "card-base" {
		t.Errorf("expected base printing first, got %q", matches[0].Card.ID)
	}
	if !matches[0].NeedsReview {
		t.Errorf("indistinguishable printings must require review")
	}
	if !matches[1].NeedsReview {
		t.Errorf("indistinguishable printings must require review")
	}
}

func TestClaudeDetection_NormalizesSetPrefixedCollectorNumbers(t *testing.T) {
	cases := []struct {
		detected string
		catalog  string
	}{
		{"TAEV EN027", "TAEV-EN027"},
		{"TAEV_EN27", "TAEV-EN027"},
		{"LON 78", "LON-078"},
		{"OP01 001", "OP01-001"},
	}
	for _, tc := range cases {
		t.Run(tc.detected, func(t *testing.T) {
			info := &claudedetect.DetectedCardInfo{
				Game:            "yugioh",
				Name:            "Crystal Beast Ruby Carbuncle",
				CollectorNumber: tc.detected,
				Set:             "Force of the Breaker",
				Language:        "en",
				Confidence:      0.95,
			}
			cards := []models.Card{{
				ID:              "card-1",
				Name:            "Crystal Beast Ruby Carbuncle",
				CollectorNumber: tc.catalog,
				Set:             "Force of the Breaker",
				SetCode:         "TAEV",
				Language:        "en",
				Game:            "yugioh",
			}}

			matches := matchCardsWithClaude(info, cards)
			if len(matches) != 1 {
				t.Fatalf("expected one candidate, got %d", len(matches))
			}
			if matches[0].Confidence != 99.0 {
				t.Errorf("expected name+number+set 99, got %f", matches[0].Confidence)
			}
		})
	}
}

func TestClaudeDetection_IgnoresWhitespaceInPrintedName(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "pokemon",
		Name:            "かがやくヒスイオオニューラ",
		CollectorNumber: "046/071",
		Set:             "S10a",
		Language:        "ja",
		Confidence:      0.93,
	}
	cards := []models.Card{{
		ID:              "s10a-046",
		Name:            "かがやくヒスイ オオニューラ",
		CollectorNumber: "046/071",
		Set:             "S10a",
		SetCode:         "S10a",
		Language:        "ja",
		Game:            "pokemon",
	}}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 1 {
		t.Fatalf("expected one candidate, got %d", len(matches))
	}
	if matches[0].Confidence != 99.0 {
		t.Errorf("expected whitespace-insensitive name match at 99, got %f", matches[0].Confidence)
	}
}

func TestClaudeDetection_PrintedFractionMatchesBareCatalogNumber(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "pokemon",
		Name:            "Alakazam",
		CollectorNumber: "1/102",
		Set:             "Base Set",
		Language:        "en",
		Confidence:      0.95,
	}
	cards := []models.Card{
		{ID: "base-1", Name: "Alakazam", CollectorNumber: "1", Set: "Base Set", SetCode: "bs", Language: "en", Game: "pokemon"},
		{ID: "base2-1", Name: "Alakazam", CollectorNumber: "1", Set: "Base Set 2", SetCode: "bs2", Language: "en", Game: "pokemon"},
		{ID: "exp-1", Name: "Alakazam", CollectorNumber: "1", Set: "Expedition Base Set", SetCode: "ex", Language: "en", Game: "pokemon"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 3 {
		t.Fatalf("expected three printings, got %d", len(matches))
	}
	if matches[0].Card.ID != "base-1" {
		t.Fatalf("expected Base Set printing first, got %q", matches[0].Card.ID)
	}
	if matches[0].Confidence != 99.0 {
		t.Errorf("printed 1/102 must match catalog 1 at 99, got %f", matches[0].Confidence)
	}
	if matches[0].NeedsReview {
		t.Errorf("exact set tells the Base Set printing apart")
	}
}

func TestClaudeDetection_CrossSetNumberTieRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "pokemon",
		Name:            "Alakazam",
		CollectorNumber: "1/102",
		Language:        "en",
		Confidence:      0.95,
	}
	cards := []models.Card{
		{ID: "base-1", Name: "Alakazam", CollectorNumber: "1", Set: "Base Set", SetCode: "bs", Language: "en", Game: "pokemon"},
		{ID: "base2-1", Name: "Alakazam", CollectorNumber: "1", Set: "Base Set 2", SetCode: "bs2", Language: "en", Game: "pokemon"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected two printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview || !matches[1].NeedsReview {
		t.Errorf("same name+number across sets without a set must require review")
	}
}

func TestClaudeDetection_VariantAmbiguityRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "one_piece",
		Name:            "Monkey.D.Luffy",
		CollectorNumber: "OP01-001",
		Set:             "Romance Dawn",
		Language:        "en",
		Confidence:      0.96,
	}
	cards := []models.Card{
		{ID: "base", Name: "Monkey.D.Luffy", CollectorNumber: "OP01-001", Set: "Romance Dawn", SetCode: "OP01", Variant: "Normal", Language: "en", Game: "one_piece", SourceCardID: "OP01-001"},
		{ID: "alt", Name: "Monkey.D.Luffy", CollectorNumber: "OP01-001", Set: "Romance Dawn", SetCode: "OP01", Variant: "Parallel", Language: "en", Game: "one_piece", SourceCardID: "OP01-001_p1"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if matches[0].Card.ID != "base" {
		t.Errorf("expected base printing first, got %q", matches[0].Card.ID)
	}
	if !matches[0].NeedsReview {
		t.Errorf("different variants must be a review choice")
	}
}

func TestClaudeDetection_AutoUsesPrintedLanguage(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "pokemon",
		Name:            "Pikachu",
		CollectorNumber: "58",
		Set:             "Base Set",
		Language:        "deu",
		Confidence:      0.94,
	}
	cards := []models.Card{
		{ID: "de-58", Name: "Pikachu", CollectorNumber: "58", Set: "Base Set", SetCode: "bs", Language: "de", Game: "pokemon"},
		{ID: "en-58", Name: "Pikachu", CollectorNumber: "58", Set: "Base Set", SetCode: "bs", Language: "en", Game: "pokemon"},
		{ID: "fr-58", Name: "Pikachu", CollectorNumber: "58", Set: "Base Set", SetCode: "bs", Language: "fr", Game: "pokemon"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 1 {
		t.Fatalf("printed language must narrow the candidates, got %d", len(matches))
	}
	if matches[0].Card.ID != "de-58" {
		t.Errorf("expected the German printing, got %q", matches[0].Card.ID)
	}
}

func TestClaudeDetection_LanguageHintConflictIsExplicit(t *testing.T) {
	mock := &mockClaudeDetector{
		info: &claudedetect.DetectedCardInfo{
			Game:            "pokemon",
			Name:            "ピカチュウ",
			CollectorNumber: "58",
			Set:             "Base Set",
			Language:        "jpn",
			Confidence:      0.93,
		},
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.Claude = mock

	cards := []models.Card{
		{ID: "en-58", Name: "Pikachu", CollectorNumber: "58", Set: "Base Set", Language: "en", Game: "pokemon"},
	}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: testCardImage(t),
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != DetectionStatusLanguageConflict {
		t.Fatalf("expected language conflict, got %s", result.Status)
	}
	if result.Conflict == nil || result.Conflict.Field != "language" || result.Conflict.Detected != "jpn" {
		t.Fatalf("expected explicit language conflict, got %+v", result.Conflict)
	}
	if !result.BestMatchNeedsReview() {
		t.Errorf("a conflict must need review")
	}
}

func TestClaudeDetection_GenericSetSubstringDoesNotConfirmUnknownName(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game:            "magic",
		Name:            "Unleserlicher Name",
		CollectorNumber: "385",
		Set:             "Commander Legends",
		Language:        "ja",
		Confidence:      0.9,
	}
	cards := []models.Card{
		{
			ID:              "cmr-385",
			Name:            "Spirit Mantle",
			CollectorNumber: "385",
			Set:             "Commander Legends: Battle for Baldur's Gate",
			SetCode:         "cmr",
			Language:        "ja",
			Game:            "magic",
		},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 1 {
		t.Fatalf("expected one candidate, got %d", len(matches))
	}
	if matches[0].Confidence != 65.0 {
		t.Errorf("substring set alone must not score 88, got %f", matches[0].Confidence)
	}
	if !matches[0].NeedsReview {
		t.Errorf("number-only candidate must require review")
	}
}

func TestClaudeDetection_FingerprintResolvesIdenticalPrintings(t *testing.T) {
	imageBytes := testCardImage(t)
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode test image: %v", err)
	}
	fingerprint := NewFingerprintService(nil)
	hash, err := fingerprint.CalculateHash(decoded)
	if err != nil {
		t.Fatalf("hash test image: %v", err)
	}
	otherHash := hash ^ 0xFF

	info := &claudedetect.DetectedCardInfo{
		Game:            "one_piece",
		Name:            "Boa Hancock",
		CollectorNumber: "ST17-004",
		Set:             "ST-17",
		Language:        "en",
		Confidence:      0.96,
	}
	cards := []models.Card{
		{
			ID: "card-base", Name: "Boa Hancock", CollectorNumber: "ST17-004",
			Set: "ST-17", SetCode: "ST-17", Language: "en", Game: "one_piece",
			SourceCardID: "ST17-004", Phash: &otherHash,
		},
		{
			ID: "card-alt", Name: "Boa Hancock", CollectorNumber: "ST17-004",
			Set: "ST-17", SetCode: "ST-17", Language: "en", Game: "one_piece",
			SourceCardID: "ST17-004_p1", Phash: &hash,
		},
	}

	pipeline := NewDetectionPipeline(fingerprint, nil)
	pipeline.Claude = &mockClaudeDetector{info: info}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGOnePiece, Language: models.LanguageEnglish},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "card-alt" {
		t.Fatalf("expected the photographed printing, got %q", result.BestMatchID())
	}
	if result.BestMatchNeedsReview() {
		t.Errorf("a decisive fingerprint must clear the review flag")
	}
	if result.Status != DetectionStatusMatched {
		t.Errorf("expected matched, got %s", result.Status)
	}
}

func TestClaudeDetection_UniqueNameWithoutNumberMatches(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "magic", Name: "Generous Visitor", Language: "eng", Confidence: 0.9,
	}
	cards := []models.Card{
		{ID: "gv-185", Name: "Generous Visitor", CollectorNumber: "185", Set: "Kamigawa: Neon Dynasty", SetCode: "neo", Language: "en", Game: "magic"},
		{ID: "other", Name: "Other Card", CollectorNumber: "12", Set: "Kamigawa: Neon Dynasty", SetCode: "neo", Language: "en", Game: "magic"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 || matches[0].Card.ID != "gv-185" {
		t.Fatalf("expected the unique name match, got %+v", matches)
	}
	if matches[0].NeedsReview {
		t.Errorf("a name that is unique in scope must not require review")
	}
}

func TestClaudeDetection_DroppedVariantSuffixRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "magic", Name: "Meeting of the Five", CollectorNumber: "202",
		Set: "Streets of New Capenna", SetCode: "snc", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "snc-202", Name: "Meeting of the Five", CollectorNumber: "202", Set: "Streets of New Capenna", SetCode: "snc", Language: "en", Game: "magic", SourceCardID: "202"},
		{ID: "snc-202s", Name: "Meeting of the Five", CollectorNumber: "202s", Set: "Streets of New Capenna", SetCode: "snc", Language: "en", Game: "magic", SourceCardID: "202s"},
	}

	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a dropped variant suffix must ask which printing was photographed")
	}
}

func TestClaudeDetection_FingerprintOverridesSetEvidence(t *testing.T) {
	imageBytes := testCardImage(t)
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode test image: %v", err)
	}
	fingerprint := NewFingerprintService(nil)
	hash, err := fingerprint.CalculateHash(decoded)
	if err != nil {
		t.Fatalf("hash test image: %v", err)
	}
	otherHash := hash ^ 0xFF

	info := &claudedetect.DetectedCardInfo{
		Game: "one_piece", Name: "Boa Hancock", CollectorNumber: "ST17-004",
		Set: "ST-17", SetCode: "ST-17", Language: "eng", Confidence: 0.96,
	}
	cards := []models.Card{
		{
			ID: "card-base", Name: "Boa Hancock", CollectorNumber: "ST17-004",
			Set: "ST-17", SetCode: "ST-17", Language: "en", Game: "one_piece",
			SourceCardID: "ST17-004", Phash: &otherHash,
		},
		{
			ID: "card-alt", Name: "Boa Hancock", CollectorNumber: "ST17-004",
			Set: "EB-05", SetCode: "EB-05", Language: "en", Game: "one_piece",
			SourceCardID: "ST17-004_p1", Phash: &hash,
		},
	}
	pipeline := NewDetectionPipeline(fingerprint, nil)
	pipeline.Claude = &mockClaudeDetector{info: info}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGOnePiece, Language: models.LanguageEnglish},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "card-alt" {
		t.Fatalf("expected the photographed alt-art row, got %q", result.BestMatchID())
	}
	if result.BestMatchNeedsReview() || result.Status != DetectionStatusMatched {
		t.Errorf("a decisive fingerprint must auto-match, got status=%s review=%v", result.Status, result.BestMatchNeedsReview())
	}
}

func TestClaudeDetection_NameVariantSiblingBlocksUniqueAuto(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "alpha_clash", Name: "Sadistic Strategy", Language: "eng", Confidence: 0.92,
	}
	cards := []models.Card{
		{ID: "base", Name: "Sadistic Strategy", CollectorNumber: "AC4-092", Set: "Alpha Clash 4", SetCode: "AC4", Language: "en", Game: "alpha_clash"},
		{ID: "iconic", Name: "Sadistic Strategy Iconic Art", CollectorNumber: "AC4-161", Set: "Alpha Clash 4", SetCode: "AC4", Language: "en", Game: "alpha_clash"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) == 0 {
		t.Fatalf("expected a candidate")
	}
	if !matches[0].NeedsReview {
		t.Errorf("a name-only read with a variant-name sibling must not auto-match")
	}
}

func TestClaudeDetection_PlusVariantSuffixRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "godzilla", Name: "Operation Wadatsumi", CollectorNumber: "ESD01-015",
		Set: "Godzilla", SetCode: "ESD01", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "base", Name: "Operation Wadatsumi", CollectorNumber: "ESD01-015", Set: "Godzilla", SetCode: "ESD01", Language: "en", Game: "godzilla"},
		{ID: "plus", Name: "Operation Wadatsumi", CollectorNumber: "ESD01-015+", Set: "Godzilla", SetCode: "ESD01", Language: "en", Game: "godzilla"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a dropped + suffix must ask which printing was photographed")
	}
}

func TestClaudeDetection_VariantLetterBeforeSlashRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "riftbound", Name: "Rumble Hotheaded", CollectorNumber: "026/221",
		Set: "Origins", SetCode: "OGN", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "base", Name: "Rumble Hotheaded", CollectorNumber: "026/221", Set: "Origins", SetCode: "OGN", Language: "en", Game: "riftbound"},
		{ID: "alt", Name: "Rumble Hotheaded Alternate Art", CollectorNumber: "026a/221", Set: "Origins", SetCode: "OGN", Language: "en", Game: "riftbound"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a dropped alternate-art letter must require review")
	}
}

func TestClaudeDetection_NameSuffixWithSameNumberRequiresReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "cookie_run", Name: "Blue Slushy Cookie", CollectorNumber: "BS6-007",
		Set: "Braverse 6", SetCode: "BS6", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "base", Name: "Blue Slushy Cookie", CollectorNumber: "BS6-007", Set: "Braverse 6", SetCode: "BS6", Language: "en", Game: "cookie_run"},
		{ID: "sec", Name: "Blue Slushy Cookie SEC", CollectorNumber: "BS6-007", Set: "Braverse 6", SetCode: "BS6", Language: "en", Game: "cookie_run"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a SEC name variant sharing the number must require review")
	}
}

func TestClaudeDetection_FingerprintResolvesDroppedVariantSuffix(t *testing.T) {
	imageBytes := testCardImage(t)
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode test image: %v", err)
	}
	fingerprint := NewFingerprintService(nil)
	hash, err := fingerprint.CalculateHash(decoded)
	if err != nil {
		t.Fatalf("hash test image: %v", err)
	}
	otherHash := hash ^ 0xFF

	info := &claudedetect.DetectedCardInfo{
		Game: "magic", Name: "Meeting of the Five", CollectorNumber: "202",
		Set: "Streets of New Capenna", SetCode: "snc", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{
			ID: "snc-202", Name: "Meeting of the Five", CollectorNumber: "202",
			Set: "Streets of New Capenna", SetCode: "snc", Language: "en", Game: "magic",
			SourceCardID: "202", Phash: &otherHash,
		},
		{
			ID: "snc-202s", Name: "Meeting of the Five", CollectorNumber: "202s",
			Set: "Streets of New Capenna", SetCode: "snc", Language: "en", Game: "magic",
			SourceCardID: "202s", Phash: &hash,
		},
	}
	pipeline := NewDetectionPipeline(fingerprint, nil)
	pipeline.Claude = &mockClaudeDetector{info: info}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGMagic, Language: models.LanguageEnglish},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BestMatchID() != "snc-202s" {
		t.Fatalf("expected the photographed variant printing, got %q", result.BestMatchID())
	}
	if result.BestMatchNeedsReview() || result.Status != DetectionStatusMatched {
		t.Errorf("a decisive hash must auto-match the variant, got status=%s review=%v", result.Status, result.BestMatchNeedsReview())
	}
}

func TestClaudeDetection_ArtworkMismatchForcesReview(t *testing.T) {
	imageBytes := testCardImage(t)
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode test image: %v", err)
	}
	fingerprint := NewFingerprintService(nil)
	hash, err := fingerprint.CalculateHash(decoded)
	if err != nil {
		t.Fatalf("hash test image: %v", err)
	}
	wrongHash := hash ^ 0xFFFF // 16 bits away, well beyond the potential radius

	info := &claudedetect.DetectedCardInfo{
		Game: "hololive", Name: "森カリオペ", CollectorNumber: "hSD18-008",
		Set: "hSD18", SetCode: "hSD18", Language: "jpn", Confidence: 0.96,
	}
	cards := []models.Card{{
		ID: "card-008", Name: "森カリオペ", CollectorNumber: "hSD18-008",
		Set: "hSD18", SetCode: "hSD18", Language: "ja", Game: "hololive",
		SourceCardID: "hSD18-008", Phash: &wrongHash,
	}}
	pipeline := NewDetectionPipeline(fingerprint, nil)
	pipeline.Claude = &mockClaudeDetector{info: info}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGHololive, Language: models.LanguageJapanese},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.BestMatchNeedsReview() || result.Status != DetectionStatusNeedsReview {
		t.Errorf("a confident text match whose artwork does not match must be review, got %s", result.Status)
	}
}

func TestClaudeDetection_ArtworkMatchKeepsAutoMatch(t *testing.T) {
	imageBytes := testCardImage(t)
	decoded, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode test image: %v", err)
	}
	fingerprint := NewFingerprintService(nil)
	hash, err := fingerprint.CalculateHash(decoded)
	if err != nil {
		t.Fatalf("hash test image: %v", err)
	}

	info := &claudedetect.DetectedCardInfo{
		Game: "hololive", Name: "森カリオペ", CollectorNumber: "hSD18-008",
		Set: "hSD18", SetCode: "hSD18", Language: "jpn", Confidence: 0.96,
	}
	cards := []models.Card{{
		ID: "card-008", Name: "森カリオペ", CollectorNumber: "hSD18-008",
		Set: "hSD18", SetCode: "hSD18", Language: "ja", Game: "hololive",
		SourceCardID: "hSD18-008", Phash: &hash,
	}}
	pipeline := NewDetectionPipeline(fingerprint, nil)
	pipeline.Claude = &mockClaudeDetector{info: info}
	result, err := pipeline.detectClaude(context.Background(), DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGHololive, Language: models.LanguageJapanese},
	}, false, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != DetectionStatusMatched {
		t.Errorf("a matching artwork must keep the auto match, got %s", result.Status)
	}
}

func TestClaudeDetection_VariantMatchSelectsFoilPrinting(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "flesh_and_blood", Name: "Short and Sharp", CollectorNumber: "OUT155",
		Set: "Outsiders", SetCode: "OUT", Variant: "Rainbow Foil", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "normal", Name: "Short and Sharp", CollectorNumber: "OUT155", Set: "Outsiders", SetCode: "OUT", Variant: "Normal", Language: "en", Game: "flesh_and_blood", SourceCardID: "OUT155"},
		{ID: "rainbow", Name: "Short and Sharp", CollectorNumber: "OUT155", Set: "Outsiders", SetCode: "OUT", Variant: "Rainbow Foil", Language: "en", Game: "flesh_and_blood", SourceCardID: "OUT155-RF"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if matches[0].Card.ID != "rainbow" {
		t.Fatalf("expected the rainbow foil printing first, got %q", matches[0].Card.ID)
	}
	if matches[0].NeedsReview {
		t.Errorf("an explicit finish that matches one printing should auto-match")
	}
}

func TestClaudeDetection_GenericFoilDoesNotPickSpecificFoil(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "flesh_and_blood", Name: "Short and Sharp", CollectorNumber: "OUT155",
		Set: "Outsiders", SetCode: "OUT", Variant: "Foil", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "normal", Name: "Short and Sharp", CollectorNumber: "OUT155", Set: "Outsiders", SetCode: "OUT", Variant: "Normal", Language: "en", Game: "flesh_and_blood", SourceCardID: "OUT155"},
		{ID: "rainbow", Name: "Short and Sharp", CollectorNumber: "OUT155", Set: "Outsiders", SetCode: "OUT", Variant: "Rainbow Foil", Language: "en", Game: "flesh_and_blood", SourceCardID: "OUT155-RF"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 2 {
		t.Fatalf("expected both printings, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a generic Foil must not silently choose a specific foil type")
	}
}

func TestClaudeDetection_VariantConflictWithSingleCandidateNeedsReview(t *testing.T) {
	info := &claudedetect.DetectedCardInfo{
		Game: "flesh_and_blood", Name: "Short and Sharp", CollectorNumber: "OUT155",
		Set: "Outsiders", SetCode: "OUT", Variant: "Rainbow Foil", Language: "eng", Confidence: 0.95,
	}
	cards := []models.Card{
		{ID: "normal", Name: "Short and Sharp", CollectorNumber: "OUT155", Set: "Outsiders", SetCode: "OUT", Variant: "Normal", Language: "en", Game: "flesh_and_blood", SourceCardID: "OUT155"},
	}
	matches := matchCardsWithClaude(info, cards)
	if len(matches) != 1 {
		t.Fatalf("expected one candidate, got %d", len(matches))
	}
	if !matches[0].NeedsReview {
		t.Errorf("a finish the catalog contradicts must not auto-match")
	}
}
