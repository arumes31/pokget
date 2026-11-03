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

// Command scan_matrix samples real catalog cards per game and language and runs
// them through the production detection pipeline with the configured Claude
// detector. It needs the same database and Claude environment as the server and
// is meant for measuring detection accuracy, not for serving traffic.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/db"
	"pokget/internal/models"
	"pokget/internal/service"
)

const maxSampleImageBytes = 8 << 20

type sampleCard struct {
	Card models.Card
}

type resultRow struct {
	Game            string   `json:"game"`
	Language        string   `json:"language"`
	ExpectedID      string   `json:"expected_id"`
	ExpectedName    string   `json:"expected_name"`
	ExpectedNumber  string   `json:"expected_number"`
	ActualID        string   `json:"actual_id"`
	ActualName      string   `json:"actual_name"`
	ActualNumber    string   `json:"actual_number"`
	Status          string   `json:"status"`
	Confidence      float64  `json:"confidence"`
	Exact           bool     `json:"exact"`
	SamePrinting    bool     `json:"same_printing"`
	ReviewCandidate bool     `json:"review_candidate"`
	DurationMS      int64    `json:"duration_ms"`
	Error           string   `json:"error,omitempty"`
	TopAlternative  string   `json:"top_alternative,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

type summary struct {
	Game         string `json:"game"`
	Language     string `json:"language"`
	Sampled      int    `json:"sampled"`
	Exact        int    `json:"exact"`
	SamePrinting int    `json:"same_printing"`
	Review       int    `json:"review"`
	Wrong        int    `json:"wrong"`
	Errors       int    `json:"errors"`
}

func main() {
	gameFlag := flag.String("game", "", "game slug; empty scans every game present in the catalog")
	languageFlag := flag.String("language", "", "language code; empty scans every language with cards")
	per := flag.Int("per", 5, "sampled cards per game/language")
	seed := flag.Int64("seed", 0, "sampling seed; 0 uses the current time")
	timeout := flag.Duration("timeout", 90*time.Second, "timeout for one scan")
	asJSON := flag.Bool("json", false, "emit JSON instead of text")
	flag.Parse()

	if err := run(*gameFlag, *languageFlag, *per, *seed, *timeout, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, "scan_matrix:", err)
		os.Exit(1)
	}
}

func run(gameFlag, languageFlag string, per int, seed int64, timeout time.Duration, asJSON bool) error {
	if per < 1 {
		return errors.New("per must be at least 1")
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	database, err := db.Connect()
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() { _ = database.Close() }()

	detector, err := newDetector(timeout)
	if err != nil {
		return err
	}

	combinations, err := scopeCombinations(database, gameFlag, languageFlag)
	if err != nil {
		return err
	}
	if len(combinations) == 0 {
		return errors.New("no active catalog cards match the requested scope")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	rows := make([]resultRow, 0, len(combinations)*per)
	summaries := make([]summary, 0, len(combinations))
	for _, combination := range combinations {
		scope, err := parseScope(combination.game, combination.language)
		if err != nil {
			return err
		}
		cards, err := scopeCards(database, combination.game, combination.language)
		if err != nil {
			return fmt.Errorf("load %s/%s cards: %w", combination.game, combination.language, err)
		}
		samples, err := sampleCards(database, combination.game, combination.language, per, seed)
		if err != nil {
			return fmt.Errorf("sample %s/%s cards: %w", combination.game, combination.language, err)
		}
		pipeline := service.NewDetectionPipeline(nil, nil)
		pipeline.Claude = detector
		combinationSummary := summary{Game: combination.game, Language: combination.language}
		for _, sample := range samples {
			row := scanOne(context.Background(), client, pipeline, scope, cards, combination, sample, timeout)
			combinationSummary.Sampled++
			switch {
			case row.Error != "":
				combinationSummary.Errors++
			case row.Exact:
				combinationSummary.Exact++
			case row.SamePrinting:
				combinationSummary.SamePrinting++
			case row.ReviewCandidate:
				combinationSummary.Review++
			default:
				combinationSummary.Wrong++
			}
			rows = append(rows, row)
			if !asJSON {
				printRow(row)
			}
		}
		summaries = append(summaries, combinationSummary)
	}

	if asJSON {
		payload := map[string]any{"seed": seed, "rows": rows, "summary": summaries}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Println()
	printSummary(summaries)
	return nil
}

func newDetector(timeout time.Duration) (*claudedetect.Client, error) {
	apiKey := strings.TrimSpace(os.Getenv("CLAUDE_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("CLAUDE_API_KEY is required")
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = claudedetect.DefaultModel
	}
	detector, err := claudedetect.New(claudedetect.Config{
		APIKey:        apiKey,
		Model:         model,
		BaseURL:       os.Getenv("CLAUDE_BASE_URL"),
		Timeout:       timeout,
		PromptCaching: true,
	})
	if err != nil {
		return nil, fmt.Errorf("configure detector: %w", err)
	}
	slog.Info("detector ready", "model", detector.Model())
	return detector, nil
}

type combination struct {
	game     string
	language string
}

func scopeCombinations(database *sql.DB, gameFilter, languageFilter string) ([]combination, error) {
	query := `SELECT game, language, count(*) FROM cards
		WHERE catalog_active = TRUE AND superseded_by_card_id IS NULL
		  AND coalesce(image_url,'') <> '' AND coalesce(name,'') <> ''
		  AND ($1 = '' OR game = $1) AND ($2 = '' OR language = $2)
		GROUP BY game, language HAVING count(*) >= 5 ORDER BY game, language`
	rows, err := database.Query(query, gameFilter, languageFilter)
	if err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var combinations []combination
	for rows.Next() {
		var item combination
		var count int
		if err := rows.Scan(&item.game, &item.language, &count); err != nil {
			return nil, err
		}
		combinations = append(combinations, item)
	}
	return combinations, rows.Err()
}

func parseScope(game, language string) (service.ScanScope, error) {
	tcg, err := models.ParseTCG(game)
	if err != nil {
		return service.ScanScope{}, err
	}
	lang, err := models.ParseLanguage(language)
	if err != nil {
		return service.ScanScope{}, err
	}
	return service.ScanScope{TCG: tcg, Language: lang}, nil
}

const cardColumns = `id, name, set_name, coalesce(set_code,''), coalesce(collector_number,''),
	language, game, coalesce(variant,''), coalesce(image_url,'')`

func scanCard(rows *sql.Rows) (sampleCard, error) {
	var card models.Card
	if err := rows.Scan(&card.ID, &card.Name, &card.Set, &card.SetCode, &card.CollectorNumber,
		&card.Language, &card.Game, &card.Variant, &card.ImageURL); err != nil {
		return sampleCard{}, err
	}
	return sampleCard{Card: card}, nil
}

func scopeCards(database *sql.DB, game, language string) ([]models.Card, error) {
	query := `SELECT ` + cardColumns + ` FROM cards
		WHERE catalog_active = TRUE AND superseded_by_card_id IS NULL
		  AND game = $1 AND language = $2`
	rows, err := database.Query(query, game, language)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cards []models.Card
	for rows.Next() {
		scanned, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, scanned.Card)
	}
	return cards, rows.Err()
}

func sampleCards(database *sql.DB, game, language string, per int, seed int64) ([]sampleCard, error) {
	// setseed makes "ORDER BY random()" reproducible for a given seed.
	seedFraction := float64(seed%1_000_000_007) / 1_000_000_007.0
	if _, err := database.Exec(`SELECT setseed($1)`, seedFraction); err != nil {
		return nil, err
	}
	query := `SELECT ` + cardColumns + ` FROM cards
		WHERE catalog_active = TRUE AND superseded_by_card_id IS NULL
		  AND game = $1 AND language = $2
		  AND coalesce(image_url,'') <> '' AND coalesce(name,'') <> ''
		ORDER BY random() LIMIT $3`
	rows, err := database.Query(query, game, language, per)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var samples []sampleCard
	for rows.Next() {
		scanned, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		samples = append(samples, scanned)
	}
	return samples, rows.Err()
}

func scanOne(ctx context.Context, client *http.Client, pipeline *service.DetectionPipeline,
	scope service.ScanScope, cards []models.Card, combination combination, sample sampleCard,
	timeout time.Duration) resultRow {
	row := resultRow{
		Game:           combination.game,
		Language:       combination.language,
		ExpectedID:     sample.Card.ID,
		ExpectedName:   sample.Card.Name,
		ExpectedNumber: sample.Card.CollectorNumber,
	}
	started := time.Now()
	imageBytes, err := fetchImage(ctx, client, sample.Card.ImageURL)
	if err != nil {
		row.Error = err.Error()
		row.DurationMS = time.Since(started).Milliseconds()
		return row
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := pipeline.DetectScoped(scanCtx, service.DetectionRequest{
		Image: imageBytes,
		Cards: cards,
		Scope: scope,
	})
	row.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		row.Error = err.Error()
		return row
	}
	if result == nil {
		row.Error = "empty detection result"
		return row
	}
	row.Status = string(result.Status)
	if best := result.BestMatchCard(); best != nil {
		row.ActualID = best.ID
		row.ActualName = best.Name
		row.ActualNumber = best.CollectorNumber
		row.Confidence = result.BestMatchConfidence()
		if best.ID != sample.Card.ID && len(result.TopMatches) > 0 {
			row.TopAlternative = strings.TrimSpace(result.TopMatches[0].Card.Name + " " + result.TopMatches[0].Card.CollectorNumber)
		}
	}
	row.Exact = row.ActualID != "" && row.ActualID == sample.Card.ID
	row.SamePrinting = !row.Exact && strings.EqualFold(strings.TrimSpace(row.ActualName), strings.TrimSpace(sample.Card.Name)) &&
		strings.EqualFold(strings.TrimSpace(row.ActualNumber), strings.TrimSpace(sample.Card.CollectorNumber)) &&
		row.ActualName != ""
	for _, match := range result.TopMatches {
		if match.Card != nil && match.Card.ID == sample.Card.ID {
			row.ReviewCandidate = true
			break
		}
	}
	if !row.Exact {
		row.Notes = differenceNotes(sample.Card, result.OCRText)
	}
	return row
}

func differenceNotes(expected models.Card, extracted string) []string {
	var notes []string
	if expected.Name != "" && !strings.Contains(strings.ToLower(extracted), strings.ToLower(expected.Name)) {
		notes = append(notes, "printed name not found in extracted text")
	}
	if expected.CollectorNumber != "" && !strings.Contains(extracted, expected.CollectorNumber) {
		notes = append(notes, "collector number not found in extracted text")
	}
	return notes
}

func fetchImage(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("card has no image URL")
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		payload, err := fetchImageOnce(ctx, client, url)
		if err == nil {
			return payload, nil
		}
		lastErr = err
		if attempt < 3 && retryableImageError(err) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
			continue
		}
		break
	}
	return nil, lastErr
}

func retryableImageError(err error) bool {
	var statusErr *imageStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusTooManyRequests || statusErr.status >= 500
	}
	return true
}

type imageStatusError struct {
	status int
	url    string
}

func (e *imageStatusError) Error() string {
	return fmt.Sprintf("image status %d for %s", e.status, e.url)
}

func fetchImageOnce(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err == nil {
		request.Header.Set("User-Agent", "pokget-scan-matrix/1.0")
	}
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, &imageStatusError{status: response.StatusCode, url: url}
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxSampleImageBytes))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, errors.New("empty image response")
	}
	return payload, nil
}

func printRow(row resultRow) {
	marker := "WRONG"
	switch {
	case row.Error != "":
		marker = "ERROR"
	case row.Exact:
		marker = "EXACT"
	case row.ReviewCandidate:
		marker = "REVIEW"
	}
	alternative := ""
	if row.TopAlternative != "" {
		alternative = " -> " + row.TopAlternative
	}
	note := ""
	if len(row.Notes) > 0 {
		note = " (" + strings.Join(row.Notes, "; ") + ")"
	}
	errorText := ""
	if row.Error != "" {
		errorText = " " + row.Error
	}
	fmt.Printf("%-6s %-14s %-4s %-40s #%-6s got=%q #%s conf=%.0f %s%s%s\n",
		marker, row.Game, row.Language, truncate(row.ExpectedName, 40), row.ExpectedNumber,
		truncate(row.ActualName, 40), row.ActualNumber, row.Confidence, row.Status, alternative, note+errorText)
}

func printSummary(summaries []summary) {
	fmt.Println("game                 language  sampled  exact  same#  review  wrong  errors  exact%")
	var totalSampled, totalExact, totalSame int
	for _, item := range summaries {
		percent := 0.0
		if item.Sampled > 0 {
			percent = 100 * float64(item.Exact) / float64(item.Sampled)
		}
		totalSampled += item.Sampled
		totalExact += item.Exact
		totalSame += item.SamePrinting
		fmt.Printf("%-20s %-9s %7d %6d %6d %7d %6d %7d %6.0f%%\n",
			item.Game, item.Language, item.Sampled, item.Exact, item.SamePrinting, item.Review, item.Wrong, item.Errors, percent)
	}
	if len(summaries) > 0 {
		percent := 0.0
		if totalSampled > 0 {
			percent = 100 * float64(totalExact) / float64(totalSampled)
		}
		fmt.Printf("%-20s %-9s %7d %6d %6d\n", "TOTAL", "", totalSampled, totalExact, totalSame)
		fmt.Printf("exact match rate: %.1f%% (name+number rate: %.1f%%)\n", percent,
			100*float64(totalExact+totalSame)/float64(totalSampled))
	}
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}