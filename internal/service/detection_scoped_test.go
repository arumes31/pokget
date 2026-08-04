package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"pokget/internal/models"
)

func TestDetectScopedFiltersTCGLanguageAndInactiveCards(t *testing.T) {
	t.Parallel()

	active, inactive := true, false
	cards := []models.Card{
		{ID: "pokemon-en", Name: "Pikachu", Game: "pokemon", Language: "en", CatalogActive: &active},
		{ID: "pokemon-de", Name: "Pikachu", Game: "pokemon", Language: "de", CatalogActive: &active},
		{ID: "magic-en", Name: "Pikachu", Game: "magic", Language: "en", CatalogActive: &active},
		{ID: "inactive", Name: "Pikachu", Game: "pokemon", Language: "en", CatalogActive: &inactive},
	}
	pipeline := NewDetectionPipeline(nil, nil)
	var fingerprintIDs, ocrIDs []string
	ocrStarted := make(chan struct{})
	pipeline.fingerprintRunner = func(_ context.Context, _ []byte, scopedCards []models.Card, scope *ScanScope) (*MatchResult, error) {
		fingerprintIDs = cardIDs(scopedCards)
		if scope == nil || scope.TCG != models.TCGPokemon || scope.Language != models.LanguageEnglish {
			t.Errorf("fingerprint scope = %+v", scope)
		}
		<-ocrStarted
		return &MatchResult{
			HighConfidence: &scopedCards[0], BestDistance: 0,
			Potential: []FingerprintMatch{{Card: &scopedCards[0], Distance: 0}},
		}, nil
	}
	pipeline.ocrRunner = func(_ context.Context, _ []byte, scopedCards []models.Card, language string) (string, string, []byte, error) {
		ocrIDs = cardIDs(scopedCards)
		close(ocrStarted)
		if language != "eng" {
			t.Errorf("OCR language = %q, want eng", language)
		}
		return "Pikachu", "pokemon-en", nil, nil
	}

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: []byte("image"), Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fingerprintIDs, []string{"pokemon-en"}) || !slices.Equal(ocrIDs, []string{"pokemon-en"}) {
		t.Fatalf("stage cards = fingerprint %v, OCR %v", fingerprintIDs, ocrIDs)
	}
	if result.Status != DetectionStatusMatched || result.BestMatchID() != "pokemon-en" {
		t.Fatalf("result = status %q, ID %q", result.Status, result.BestMatchID())
	}
	if len(result.Metrics.Stages) != 1 || result.Metrics.Stages[0].Name != "fingerprint" {
		t.Fatalf("stages = %+v, want fingerprint fast-path metric", result.Metrics.Stages)
	}
}

func TestDetectScopedAutoLanguageKeepsTCGBoundary(t *testing.T) {
	t.Parallel()

	cards := []models.Card{
		{ID: "pokemon-en", Name: "Pikachu", Game: "pokemon", Language: "en"},
		{ID: "pokemon-ja", Name: "Pikachu", Game: "pokemon", Language: "ja"},
		{ID: "magic-en", Name: "Pikachu", Game: "magic", Language: "en"},
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(_ context.Context, _ []byte, scopedCards []models.Card, scope *ScanScope) (*MatchResult, error) {
		if scope == nil || scope.TCG != models.TCGPokemon || scope.Language != models.LanguageAny {
			t.Fatalf("fingerprint scope = %+v", scope)
		}
		if got := cardIDs(scopedCards); !slices.Equal(got, []string{"pokemon-en", "pokemon-ja"}) {
			t.Fatalf("fingerprint cards = %v", got)
		}
		return nil, nil
	}
	pipeline.ocrRunner = func(_ context.Context, _ []byte, scopedCards []models.Card, language string) (string, string, []byte, error) {
		if got := cardIDs(scopedCards); !slices.Equal(got, []string{"pokemon-en", "pokemon-ja"}) {
			t.Fatalf("OCR cards = %v", got)
		}
		if language != models.LanguageAny.TesseractCode() {
			t.Fatalf("OCR language = %q", language)
		}
		return "", "Unknown Card", nil, nil
	}

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: []byte("image"),
		Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageAny},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DetectionStatusNoMatch {
		t.Fatalf("status = %q, want %q", result.Status, DetectionStatusNoMatch)
	}
}

func TestDetectScopedRunsFingerprintAndOCRConcurrently(t *testing.T) {
	t.Parallel()

	fingerprintStarted := make(chan struct{})
	ocrStarted := make(chan struct{})
	release := make(chan struct{})
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ *ScanScope) (*MatchResult, error) {
		close(fingerprintStarted)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pipeline.ocrRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ string) (string, string, []byte, error) {
		close(ocrStarted)
		select {
		case <-release:
			return "", "Unknown Card", nil, nil
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := pipeline.DetectScoped(context.Background(), validScopedDetectionRequest())
		done <- err
	}()
	awaitSignal(t, fingerprintStarted, "fingerprint stage")
	awaitSignal(t, ocrStarted, "OCR stage")
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDetectScopedFingerprintFastPathCancelsSlowOCR(t *testing.T) {
	t.Parallel()

	ocrStarted := make(chan struct{})
	ocrCanceled := make(chan struct{})
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(_ context.Context, _ []byte, cards []models.Card, _ *ScanScope) (*MatchResult, error) {
		<-ocrStarted
		return &MatchResult{
			HighConfidence: &cards[0], BestDistance: 0,
			Potential: []FingerprintMatch{{Card: &cards[0], Distance: 0}},
		}, nil
	}
	pipeline.ocrRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ string) (string, string, []byte, error) {
		close(ocrStarted)
		<-ctx.Done()
		close(ocrCanceled)
		return "", "", nil, ctx.Err()
	}

	result, err := pipeline.DetectScoped(context.Background(), validScopedDetectionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.BestMatchID() != "pikachu-025" || len(result.Metrics.Stages) != 1 {
		t.Fatalf("fast-path result = %+v", result)
	}
	awaitSignal(t, ocrCanceled, "OCR cancellation")
}

func TestDetectScopedExactFingerprintCollisionUsesOCR(t *testing.T) {
	t.Parallel()

	cards := []models.Card{
		{ID: "printing-a", Name: "Shared Art", Game: "pokemon", Language: "en"},
		{ID: "printing-b", Name: "Shared Art", Game: "pokemon", Language: "en"},
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(_ context.Context, _ []byte, cards []models.Card, _ *ScanScope) (*MatchResult, error) {
		return &MatchResult{Potential: []FingerprintMatch{
			{Card: &cards[0], Distance: 0},
			{Card: &cards[1], Distance: 0},
		}}, nil
	}
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "Shared Art printing-b", "printing-b", nil, nil
	}

	result, err := pipeline.DetectScoped(context.Background(), DetectionRequest{
		Image: []byte("image"), Cards: cards,
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BestMatchID() != "printing-b" || result.BestMatchNeedsReview() {
		t.Fatalf("collision result = ID %q, confidence %.2f, review %t",
			result.BestMatchID(), result.BestMatchConfidence(), result.BestMatchNeedsReview())
	}
}

func TestDetectScopedCancellationStopsIndependentStages(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	blockingRunner := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	pipeline := NewDetectionPipeline(nil, nil)
	pipeline.fingerprintRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ *ScanScope) (*MatchResult, error) {
		return nil, blockingRunner(ctx)
	}
	pipeline.ocrRunner = func(ctx context.Context, _ []byte, _ []models.Card, _ string) (string, string, []byte, error) {
		return "", "", nil, blockingRunner(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		result *DetectionResult
		err    error
	}, 1)
	go func() {
		result, err := pipeline.DetectScoped(ctx, validScopedDetectionRequest())
		done <- struct {
			result *DetectionResult
			err    error
		}{result, err}
	}()
	awaitSignal(t, started, "first stage")
	awaitSignal(t, started, "second stage")
	cancel()
	select {
	case output := <-done:
		if !errors.Is(output.err, context.Canceled) || output.result.Status != DetectionStatusCanceled {
			t.Fatalf("canceled result = (%+v, %v)", output.result, output.err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled pipeline did not return promptly")
	}
}

func TestDetectScopedReturnsStatusAndErrorSeparately(t *testing.T) {
	t.Parallel()

	pipeline := NewDetectionPipeline(nil, nil)
	fingerprintErr := errors.New("fingerprint unavailable")
	ocrErr := errors.New("OCR unavailable")
	pipeline.fingerprintRunner = func(context.Context, []byte, []models.Card, *ScanScope) (*MatchResult, error) {
		return nil, fingerprintErr
	}
	pipeline.ocrRunner = func(context.Context, []byte, []models.Card, string) (string, string, []byte, error) {
		return "", "", nil, ocrErr
	}
	result, err := pipeline.DetectScoped(context.Background(), validScopedDetectionRequest())
	if !errors.Is(err, fingerprintErr) || !errors.Is(err, ocrErr) {
		t.Fatalf("error = %v, want joined stage failures", err)
	}
	if result.Status != DetectionStatusFailed || len(result.TopMatches) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDetectScopedRejectsMissingOrMismatchedScope(t *testing.T) {
	t.Parallel()

	tests := []DetectionRequest{
		{Image: []byte("image"), Cards: validScopedDetectionRequest().Cards},
		{Image: []byte("image"), Cards: validScopedDetectionRequest().Cards, Scope: ScanScope{TCG: models.TCGMagic, Language: models.LanguageEnglish}},
	}
	for _, request := range tests {
		result, err := NewDetectionPipeline(nil, nil).DetectScoped(context.Background(), request)
		if err == nil || result.Status != DetectionStatusInvalidRequest {
			t.Fatalf("invalid request result = (%+v, %v)", result, err)
		}
	}
}

func validScopedDetectionRequest() DetectionRequest {
	return DetectionRequest{
		Image: []byte("image"),
		Cards: []models.Card{{ID: "pikachu-025", Name: "Pikachu", Game: "pokemon", Language: "en"}},
		Scope: ScanScope{TCG: models.TCGPokemon, Language: models.LanguageEnglish},
	}
}

func cardIDs(cards []models.Card) []string {
	ids := make([]string, len(cards))
	for index := range cards {
		ids[index] = cards[index].ID
	}
	return ids
}

func awaitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
