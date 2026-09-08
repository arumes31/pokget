package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"pokget/internal/models"
)

type visionOCRFunc func(context.Context, []byte) (string, error)

func (f visionOCRFunc) Extract(ctx context.Context, b []byte) (string, error) { return f(ctx, b) }

func visionOCRPipeline(local string, provider visionOCRFunc) *DetectionPipeline {
	p := NewDetectionPipeline(nil, nil)
	p.VisionOCR = provider
	p.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) { return nil, nil }
	p.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return local, "", nil, nil
	}
	return p
}

func visionOCRImage(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 32, 48))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestVisionOCRSevenLanguagesRemainScopedAndReviewable(t *testing.T) {
	for _, tc := range []struct{ lang, name string }{
		{"en", "Charizard ex"}, {"de", "Glurak ex"}, {"fr", "Dracaufeu ex"},
		{"ja", "リザードンex"}, {"zh-hans", "喷火龙ex"}, {"zh-hant", "噴火龍ex"}, {"ko", "리자몽 ex"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			otherLanguage := "de"
			if tc.lang == "de" {
				otherLanguage = "en"
			}
			inactive := false
			calls := 0
			p := visionOCRPipeline("", func(_ context.Context, b []byte) (string, error) {
				calls++
				if len(b) < 2 || b[0] != 0xff || b[1] != 0xd8 {
					t.Error("provider must receive normalized JPEG")
				}
				return tc.name + "\n199/165", nil
			})
			cards := []models.Card{
				{ID: "target", Name: "Charizard ex", LocalizedNames: []string{tc.name}, CollectorNumber: "199", Game: "pokemon", Language: tc.lang},
				{ID: "wrong-game", Name: tc.name, CollectorNumber: "199", Game: "magic", Language: tc.lang},
				{ID: "wrong-language", Name: tc.name, CollectorNumber: "199", Game: "pokemon", Language: otherLanguage},
				{ID: "inactive", Name: tc.name, CollectorNumber: "199", Game: "pokemon", Language: tc.lang, CatalogActive: &inactive},
			}
			r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.Language(tc.lang)}})
			if err != nil || calls != 1 || r.BestMatchID() != "target" || r.Status != DetectionStatusNeedsReview || r.BestMatchConfidence() >= 70 {
				t.Fatalf("calls=%d result=%+v err=%v", calls, r, err)
			}
			if len(r.TopMatches) != 1 || r.TopMatches[0].printingEvidence || r.TopMatches[0].OCRScore.Method != "vision_ocr" || r.OCRText != "" {
				t.Fatalf("model text elevated to local evidence: %+v", r)
			}
		})
	}
}

func TestVisionOCRAnyLanguageDoesNotGuessBetweenPrintings(t *testing.T) {
	p := visionOCRPipeline("", func(context.Context, []byte) (string, error) { return "Pikachu\n025/100", nil })
	cards := []models.Card{
		{ID: "en", Name: "Pikachu", CollectorNumber: "25", Game: "pokemon", Language: "en"},
		{ID: "de", Name: "Pikachu", CollectorNumber: "25", Game: "pokemon", Language: "de"},
	}
	r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageAny}})
	if err != nil || len(r.TopMatches) != 0 {
		t.Fatalf("language was guessed: %+v, %v", r, err)
	}
}

func TestVisionOCRSetCollectorAndFingerprintConflict(t *testing.T) {
	cards := []models.Card{
		{ID: "local", Name: "Other Card", Game: "one_piece", Language: "en"},
		{ID: "model", Name: "Monkey.D.Luffy", CollectorNumber: "ST01-001", SetCode: "ST01", Game: "one_piece", Language: "en"},
	}
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "alphanumeric identifier", true: "do not promote over fingerprint"}[conflict], func(t *testing.T) {
			p := visionOCRPipeline("", func(context.Context, []byte) (string, error) { return "Monkey.D.Luffy\nST01-001", nil })
			if conflict {
				p.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
					return &MatchResult{Potential: []FingerprintMatch{{Card: &cards[0], Distance: 2}}}, nil
				}
			}
			r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGOnePiece, Language: models.LanguageEnglish}})
			want := "model"
			if conflict {
				want = "local"
			}
			if err != nil || r.BestMatchID() != want || !r.BestMatchNeedsReview() {
				t.Fatalf("conflict=%v result=%+v err=%v", conflict, r, err)
			}
		})
	}
}

func TestVisionOCRCallerCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := visionOCRPipeline("", func(modelCtx context.Context, _ []byte) (string, error) {
		cancel()
		<-modelCtx.Done()
		return "", modelCtx.Err()
	})
	cards := []models.Card{{ID: "target", Name: "Furret", Game: "pokemon", Language: "en"}}
	r, err := p.DetectScoped(ctx, DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if !errors.Is(err, context.Canceled) || r.Status != DetectionStatusCanceled {
		t.Fatalf("cancellation lost: %+v, %v", r, err)
	}
}

func TestVisionOCRRejectsUnverifiableOrAmbiguousIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		duplicate  bool
	}{
		{"name only", "Furret", false}, {"number only", "136/189", false},
		{"wrong number", "Furret\n001/165", false}, {"invented name", "Invented Card\n136/189", false},
		{"ambiguous printings", "Furret\n136/189", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := visionOCRPipeline("", func(context.Context, []byte) (string, error) { return tc.text, nil })
			cards := []models.Card{{ID: "target", Name: "Furret", CollectorNumber: "136", Game: "pokemon", Language: "en"}}
			if tc.duplicate {
				c := cards[0]
				c.ID = "other"
				cards = append(cards, c)
			}
			r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
			if err != nil || len(r.TopMatches) != 0 {
				t.Fatalf("unverified evidence accepted: %+v err=%v", r, err)
			}
		})
	}
}

func TestVisionOCRSkipsLocallyReadPrinting(t *testing.T) {
	p := visionOCRPipeline("Furret\n136/189", func(context.Context, []byte) (string, error) {
		t.Error("model must not run for local printing evidence")
		return "", nil
	})
	cards := []models.Card{{ID: "target", Name: "Furret", CollectorNumber: "136", Game: "pokemon", Language: "en"}}
	r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || r.BestMatchID() != "target" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestVisionOCRFailurePreservesLocalResult(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "provider error", true: "deadline"}[timeout], func(t *testing.T) {
			p := visionOCRPipeline("Furret", func(ctx context.Context, _ []byte) (string, error) {
				if timeout {
					<-ctx.Done()
					return "", ctx.Err()
				}
				return "", errors.New("provider unavailable")
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			cards := []models.Card{{ID: "target", Name: "Furret", Game: "pokemon", Language: "en"}}
			r, err := p.DetectScoped(ctx, DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
			if err != nil || ctx.Err() != nil || r.BestMatchID() != "target" || !r.BestMatchNeedsReview() {
				t.Fatalf("lost local fallback: %+v err=%v ctx=%v", r, err, ctx.Err())
			}
			found := false
			for _, s := range r.Metrics.Stages {
				if s.Name == "vision_ocr" && s.Error != nil {
					found = true
				}
			}
			if !found {
				t.Fatal("missing provider failure metric")
			}
		})
	}
}

func TestVisionOCRRanksAmbiguousLocalNamesWithoutAutoAccept(t *testing.T) {
	p := visionOCRPipeline("Furret", func(context.Context, []byte) (string, error) { return "Furret\n136/189", nil })
	cards := []models.Card{
		{ID: "a-other", Name: "Furret", CollectorNumber: "35", Game: "pokemon", Language: "en"},
		{ID: "z-target", Name: "Furret", CollectorNumber: "136", Game: "pokemon", Language: "en"},
	}
	r, err := p.DetectScoped(context.Background(), DetectionRequest{Image: visionOCRImage(t), Cards: cards, Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish}})
	if err != nil || r.BestMatchID() != "z-target" || !r.BestMatchNeedsReview() || r.TopMatches[0].printingEvidence {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
