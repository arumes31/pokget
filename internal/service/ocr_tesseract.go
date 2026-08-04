//go:build cgo && (linux || darwin || freebsd)

package service

import (
	"context"
	"fmt"
	"log/slog"
	"pokget/internal/db"
	"pokget/internal/models"
	"strings"
	"sync"
	"time"

	"github.com/otiai10/gosseract/v2"
	"golang.org/x/text/unicode/norm"
)

var ocrClientPool chan *gosseract.Client
var ocrClientPoolOnce sync.Once

func initOCRClientPool() {
	poolSize := max(OCRPoolSize, 1)
	ocrClientPool = make(chan *gosseract.Client, poolSize)
	for range poolSize {
		ocrClientPool <- gosseract.NewClient()
	}
	slog.Info("OCR client pool initialized", "size", poolSize)
}

func acquireOCRClient() (*gosseract.Client, error) {
	return acquireOCRClientContext(context.Background())
}

func acquireOCRClientContext(ctx context.Context) (*gosseract.Client, error) {
	ocrClientPoolOnce.Do(initOCRClientPool)
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case client := <-ocrClientPool:
		return client, nil
	case <-timer.C:
		return nil, fmt.Errorf("OCR client pool exhausted: timeout waiting for an available client")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// releaseOCRClient replaces clients that failed language initialization or
// pass setup. A poisoned native Tesseract handle is never returned to the pool.
func releaseOCRClient(client *gosseract.Client, discard bool) {
	if client == nil {
		return
	}
	if discard {
		if err := client.Close(); err != nil {
			slog.Warn("failed to close discarded OCR client", "error", err)
		}
		client = gosseract.NewClient()
	}
	select {
	case ocrClientPool <- client:
	default:
		if err := client.Close(); err != nil {
			slog.Warn("failed to close excess OCR client", "error", err)
		}
	}
}

func ProcessCardScan(imgBytes []byte, cards []models.Card, lang string, llm *LLMService) (string, string, []byte, error) {
	return ProcessCardScanContext(context.Background(), imgBytes, cards, lang, llm)
}

func ProcessCardScanContext(ctx context.Context, imgBytes []byte, cards []models.Card, lang string, llm *LLMService) (string, string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if lang == "" {
		lang = defaultOCRLanguage
	}
	config := ocrScanConfigFromContext(ctx)
	if config.Game == "" {
		if game := inferOCRGame(cards); game != "" {
			config.Game = game
			config.UseLayoutROIs = true
		}
	}

	cacheKey := makeOCRCacheKeyWithConfig(imgBytes, lang, cards, config)
	if cached, ok := ocrCache.Load(cacheKey); ok {
		entry := cached.(ocrCacheEntry)
		return entry.Text, entry.DetectedCard, entry.ProcessedImage, nil
	}

	src, _, err := decodeOCRImage(imgBytes, config)
	if err != nil {
		return "", "", nil, err
	}
	src, err = prepareOCRSource(imgBytes, src, config)
	if err != nil {
		return "", "", nil, err
	}
	passes, processedImage, err := buildOCRPasses(src, config)
	if err != nil {
		return "", "", nil, fmt.Errorf("build OCR passes: %w", err)
	}

	results := make([]ocrPassResult, 0, len(passes))
	failures := make([]error, 0, len(passes))
	for _, pass := range passes {
		if err := ctx.Err(); err != nil {
			return "", "", nil, err
		}
		text, passErr := executeOCRPass(ctx, pass, lang)
		if passErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", pass.Name, passErr))
			slog.Warn("OCR pass failed", "pass", pass.Name, "error", passErr)
			continue
		}
		results = append(results, ocrPassResult{Pass: pass, Text: text, Quality: scoreOCRText(text)})
	}
	if len(results) == 0 {
		return "", "", processedImage, &OCRAllPassesFailedError{Failures: failures}
	}

	text, evidence := combineOCRResults(results)
	text = norm.NFKC.String(text)
	detectedCard := matchOCRCard(ctx, text, evidence, cards, lang, llm)
	entry := ocrCacheEntry{Text: text, DetectedCard: detectedCard, ProcessedImage: processedImage}
	ocrCache.Store(cacheKey, entry)
	return text, detectedCard, append([]byte(nil), processedImage...), nil
}

func executeOCRPass(ctx context.Context, pass ocrPass, lang string) (string, error) {
	client, err := acquireOCRClientContext(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire client: %w", err)
	}
	discard := false
	defer func() { releaseOCRClient(client, discard) }()

	languages := strings.FieldsFunc(lang, func(r rune) bool { return r == '+' || r == ',' })
	if err := client.SetLanguage(languages...); err != nil {
		discard = true
		return "", fmt.Errorf("set language %q: %w", lang, err)
	}
	if err := client.SetPageSegMode(pageSegMode(pass.PageSegmentation)); err != nil {
		discard = true
		return "", fmt.Errorf("set page segmentation: %w", err)
	}
	if err := client.SetImageFromBytes(pass.Image); err != nil {
		discard = true
		return "", fmt.Errorf("set image: %w", err)
	}
	text, err := client.Text()
	if err != nil {
		discard = true
		return "", fmt.Errorf("recognize text: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return text, nil
}

func pageSegMode(value string) gosseract.PageSegMode {
	switch value {
	case "7":
		return gosseract.PSM_SINGLE_LINE
	case "11":
		return gosseract.PSM_SPARSE_TEXT
	default:
		return gosseract.PSM_AUTO
	}
}

func matchOCRCard(ctx context.Context, text string, evidence []ocrEvidence, cards []models.Card, lang string, llm *LLMService) string {
	detectedCard := "Unknown Card"
	normalizedText := normalizeOCRText(text, lang)

	if db.DB != nil && len(cards) == 0 {
		compactText := normalizeOCRIdentifier(text)
		var matchedID, matchedName string
		err := db.DB.QueryRow(`
			SELECT id, name FROM cards
			WHERE $1 LIKE '%' || LOWER(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(id, '-', ''), '/', ''), ' ', ''), '_', ''), '0', 'o')) || '%'
			ORDER BY LENGTH(id) DESC, id ASC
			LIMIT 1`, compactText).Scan(&matchedID, &matchedName)
		if err == nil && matchedID != "" {
			detectedCard = matchedID
		}
	}

	if detectedCard == "Unknown Card" && db.DB != nil && len(cards) == 0 {
		var name string
		err := db.DB.QueryRow(`
			SELECT name FROM cards
			WHERE word_similarity(name, $1) > 0.4
			ORDER BY word_similarity(name, $1) DESC, id ASC
			LIMIT 1`, normalizedText).Scan(&name)
		if err == nil && (!isCJKLanguage(lang) || corroboratedName(evidence, name, lang)) {
			detectedCard = name
		}
	}

	if detectedCard == "Unknown Card" && len(cards) > 0 {
		detectedCard = localMatchResult(evidence, cards, lang)
	}
	if detectedCard == "Unknown Card" && llm != nil {
		if match, err := llm.FuzzyMatchCardContext(ctx, normalizedText, cards); err == nil && match != "Unknown Card" {
			detectedCard = match
		}
	}
	if detectedCard == "Unknown Card" {
		if fallback, err := fallbackExtract(text); err == nil && fallback != "Unknown Card" &&
			(!isCJKLanguage(lang) || corroboratedName(evidence, fallback, lang)) {
			detectedCard = fallback
		}
	}
	return detectedCard
}

func corroboratedName(evidence []ocrEvidence, name, lang string) bool {
	passes := make(map[string]struct{})
	normalizedName := normalizeOCRText(name, lang)
	for _, item := range evidence {
		if fuzzySubstringMatch(normalizeOCRText(item.Text, lang), normalizedName) {
			passes[item.Pass] = struct{}{}
		}
	}
	return len(passes) >= 2
}
