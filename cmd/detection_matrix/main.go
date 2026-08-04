package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pokget/internal/db"
	"pokget/internal/detectiontest"
)

const defaultFixtureDir = "artifacts/detection/v2-seed-20260804-count-6"

var (
	requiredGameSlugs = []string{
		"pokemon",
		"magic",
		"one-piece",
		"lorcana",
		"weiss-schwarz",
		"yu-gi-oh",
	}
	requiredVariants = []string{
		"source",
		"clean",
		"blur",
		"resize",
		"rotate",
		"brightness",
		"jpeg",
	}
)

type scanResponse struct {
	Detected    string  `json:"detected"`
	ID          string  `json:"id"`
	Confidence  float64 `json:"confidence"`
	NeedsReview *bool   `json:"needs_review"`
}

type testResult struct {
	Game       string  `json:"game"`
	Variant    string  `json:"variant"`
	ExpectedID string  `json:"expected_id"`
	ActualID   string  `json:"actual_id"`
	Expected   string  `json:"expected_name"`
	Actual     string  `json:"actual_name"`
	Confidence float64 `json:"confidence"`
	Passed     bool    `json:"passed"`
	Error      string  `json:"error,omitempty"`
}

type summary struct {
	Total   int          `json:"total"`
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Percent float64      `json:"percent"`
	Results []testResult `json:"results"`
}

func main() {
	var fixtureDir string
	var baseURL string
	var requestTimeout time.Duration
	flag.StringVar(&fixtureDir, "fixtures", defaultFixtureDir, "generated fixture run directory")
	flag.StringVar(&baseURL, "base-url", "http://localhost:18066", "running Pokget base URL")
	flag.DurationVar(&requestTimeout, "timeout", 45*time.Second, "timeout for each scan")
	flag.Parse()

	if err := run(context.Background(), fixtureDir, strings.TrimRight(baseURL, "/"), requestTimeout, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "detection matrix failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, fixtureDir, baseURL string, requestTimeout time.Duration, output io.Writer) error {
	if requestTimeout <= 0 {
		return errors.New("request timeout must be positive")
	}
	absDir, err := filepath.Abs(fixtureDir)
	if err != nil {
		return fmt.Errorf("resolve fixture directory: %w", err)
	}
	manifest, err := loadAndVerifyRun(absDir)
	if err != nil {
		return err
	}

	database, err := db.Connect()
	if err != nil {
		return err
	}
	defer database.Close()

	expectedIDs := make(map[string]string, len(manifest.Cards))
	for _, selected := range manifest.Cards {
		id, err := resolveExpectedID(ctx, database, selected.Card)
		if err != nil {
			return err
		}
		expectedIDs[selected.Card.GameSlug] = id
	}

	client := &http.Client{Timeout: requestTimeout}
	report := summary{Results: make([]testResult, 0, len(manifest.Cards)*7)}
	for _, selected := range manifest.Cards {
		artifacts := append([]detectiontest.Artifact(nil), selected.Artifacts...)
		sort.SliceStable(artifacts, func(i, j int) bool { return artifacts[i].Variant < artifacts[j].Variant })
		for _, artifact := range artifacts {
			result := testResult{
				Game:       selected.Card.Game,
				Variant:    artifact.Variant,
				ExpectedID: expectedIDs[selected.Card.GameSlug],
				Expected:   selected.Card.Name,
			}
			response, err := scan(ctx, client, baseURL+"/api/scan", filepath.Join(absDir, filepath.FromSlash(artifact.Path)))
			if err != nil {
				result.Error = err.Error()
			} else {
				result.ActualID = response.ID
				result.Actual = response.Detected
				result.Confidence = response.Confidence
				result.Passed = acceptScanResponse(response, result.ExpectedID, result.Expected)
				if !result.Passed && *response.NeedsReview {
					result.Error = "detector marked result for review"
				}
			}
			report.Total++
			if result.Passed {
				report.Passed++
			} else {
				report.Failed++
			}
			report.Results = append(report.Results, result)
		}
	}
	if report.Total > 0 {
		report.Percent = float64(report.Passed) * 100 / float64(report.Total)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	if report.Failed != 0 {
		return fmt.Errorf("%d/%d fixture variants failed", report.Failed, report.Total)
	}
	return nil
}

func loadAndVerifyRun(directory string) (detectiontest.OutputManifest, error) {
	payload, err := os.ReadFile(filepath.Join(directory, "selection.json"))
	if err != nil {
		return detectiontest.OutputManifest{}, fmt.Errorf("read selection: %w", err)
	}
	var manifest detectiontest.OutputManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return detectiontest.OutputManifest{}, fmt.Errorf("decode selection: %w", err)
	}
	if err := detectiontest.VerifyRun(directory, manifest.Version, detectiontest.ManifestSHA256(), manifest.Seed, manifest.SelectionCount); err != nil {
		return detectiontest.OutputManifest{}, fmt.Errorf("verify fixture run: %w", err)
	}
	if err := validateAcceptanceManifest(manifest); err != nil {
		return detectiontest.OutputManifest{}, fmt.Errorf("validate acceptance fixture run: %w", err)
	}
	return manifest, nil
}

func validateAcceptanceManifest(manifest detectiontest.OutputManifest) error {
	if len(manifest.Cards) != len(requiredGameSlugs) {
		return fmt.Errorf("got %d cards, want exactly %d", len(manifest.Cards), len(requiredGameSlugs))
	}

	requiredGames := make(map[string]struct{}, len(requiredGameSlugs))
	for _, slug := range requiredGameSlugs {
		requiredGames[slug] = struct{}{}
	}
	seenGames := make(map[string]struct{}, len(manifest.Cards))
	for _, selected := range manifest.Cards {
		slug := selected.Card.GameSlug
		if _, supported := requiredGames[slug]; !supported {
			return fmt.Errorf("unsupported game slug %q", slug)
		}
		if _, duplicate := seenGames[slug]; duplicate {
			return fmt.Errorf("duplicate game slug %q", slug)
		}
		seenGames[slug] = struct{}{}
		if err := validateAcceptanceVariants(slug, selected.Artifacts); err != nil {
			return err
		}
	}
	for _, slug := range requiredGameSlugs {
		if _, present := seenGames[slug]; !present {
			return fmt.Errorf("missing required game slug %q", slug)
		}
	}
	return nil
}

func validateAcceptanceVariants(gameSlug string, artifacts []detectiontest.Artifact) error {
	if len(artifacts) != len(requiredVariants) {
		return fmt.Errorf(
			"game %q has %d variants, want exactly %d",
			gameSlug,
			len(artifacts),
			len(requiredVariants),
		)
	}

	required := make(map[string]struct{}, len(requiredVariants))
	for _, variant := range requiredVariants {
		required[variant] = struct{}{}
	}
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if _, supported := required[artifact.Variant]; !supported {
			return fmt.Errorf("game %q has unsupported variant %q", gameSlug, artifact.Variant)
		}
		if _, duplicate := seen[artifact.Variant]; duplicate {
			return fmt.Errorf("game %q has duplicate variant %q", gameSlug, artifact.Variant)
		}
		seen[artifact.Variant] = struct{}{}
	}
	for _, variant := range requiredVariants {
		if _, present := seen[variant]; !present {
			return fmt.Errorf("game %q is missing required variant %q", gameSlug, variant)
		}
	}
	return nil
}

func resolveExpectedID(ctx context.Context, database *sql.DB, card detectiontest.Card) (string, error) {
	sourceID := map[string]string{
		"pokemon":       "tcgdex",
		"magic":         "scryfall",
		"one-piece":     "onepiece_official",
		"lorcana":       "lorcanajson",
		"weiss-schwarz": "weiss_official",
		"yu-gi-oh":      "ygoprodeck",
	}[card.GameSlug]
	if sourceID == "" {
		return "", fmt.Errorf("resolve %s: unsupported game slug %q", card.Game, card.GameSlug)
	}
	sourceCardID := card.SourceID
	if card.GameSlug == "lorcana" && card.Source != "LorcanaJSON" {
		sourceCardID = card.CollectorNumber
	}

	rows, err := database.QueryContext(ctx, `
		SELECT id
		FROM cards
		WHERE source_id = $1
		  AND language = $2
		  AND name = $3
		  AND source_card_id = $4
		  AND catalog_active
		ORDER BY id`, sourceID, card.Language, card.Name, sourceCardID)
	if err != nil {
		return "", fmt.Errorf("resolve %s catalog card: %w", card.Game, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("resolve %s %q: got %d matching catalog rows, want exactly 1", card.Game, card.Name, len(ids))
	}
	return ids[0], nil
}

func scan(ctx context.Context, client *http.Client, endpoint, path string) (scanResponse, error) {
	file, err := os.Open(path)
	if err != nil {
		return scanResponse{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("card_image", filepath.Base(path))
	if err != nil {
		return scanResponse{}, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return scanResponse{}, err
	}
	if err := form.WriteField("lang", "eng"); err != nil {
		return scanResponse{}, err
	}
	if err := form.Close(); err != nil {
		return scanResponse{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return scanResponse{}, err
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		return scanResponse{}, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return scanResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		return scanResponse{}, fmt.Errorf("scan returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	var result scanResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return scanResponse{}, err
	}
	if result.NeedsReview == nil {
		return scanResponse{}, errors.New("scan response is missing boolean needs_review")
	}
	return result, nil
}

func acceptScanResponse(response scanResponse, expectedID, expectedName string) bool {
	return response.NeedsReview != nil &&
		!*response.NeedsReview &&
		response.ID == expectedID &&
		response.Detected == expectedName
}
