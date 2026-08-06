package main

import (
	"encoding/json"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"pokget/internal/db"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

type CardMetadata struct {
	Lang   string `json:"lang"`
	Name   string `json:"name"`
	Number string `json:"number"`
}

type GroupKey struct {
	Game string
	Lang string
}

type TestResult struct {
	Total   int
	Passed  int
	Skipped int
}

func main() {
	fmt.Println("==================================================")
	fmt.Println(" TCG & Language OCR Integration Test Suite")
	fmt.Println("==================================================")

	// 1. Initialize Database
	db.InitDB()
	if db.DB == nil {
		fmt.Println("Error: Failed to connect to database.")
		os.Exit(1)
	}

	// Clear cards table
	fmt.Println("Clearing database cards table...")
	_, err := db.DB.Exec("DELETE FROM cards;")
	if err != nil {
		fmt.Printf("Error clearing cards table: %v\n", err)
	}

	// 2. Load test_cards_metadata.json
	metadataPath := filepath.Join("test_cards", "test_cards_metadata.json")
	metadataBytes, err := os.ReadFile(metadataPath) // #nosec G304 -- metadataPath is fixed under the local test_cards directory.
	if err != nil {
		fmt.Printf("Error: Failed to read metadata file: %v\n", err)
		os.Exit(1)
	}

	var metadata map[string]CardMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	if err != nil {
		fmt.Printf("Error: Failed to parse metadata: %v\n", err)
		os.Exit(1)
	}

	// 2.5. Initialize LLM Service
	llm := service.NewLLMService()
	go llm.AutoSetup()
	fmt.Println("Waiting for LLM model to be ready (this may take a moment to pull)...")
	llmReady := false
	for i := 0; i < 30; i++ { // Wait up to 5 minutes (10s * 30)
		resp, err := llm.HTTPClient.Get(llm.BaseURL + "/api/tags")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if readErr != nil {
				fmt.Printf("\nFailed to read LLM readiness response: %v\n", readErr)
			} else if closeErr != nil {
				fmt.Printf("\nFailed to close LLM readiness response: %v\n", closeErr)
			} else if strings.Contains(string(body), llm.Model) {
				fmt.Printf("\nLLM model %q is ready!\n", llm.Model)
				llmReady = true
				break
			}
		}
		fmt.Print(".")
		time.Sleep(10 * time.Second)
	}
	if !llmReady {
		fmt.Printf("\nLLM model %q is not ready yet. Proceeding with Tesseract + Fuzzy String Matching.\n", llm.Model)
		llm = nil
	}

	fmt.Printf("Loaded %d card metadata entries.\n", len(metadata))

	// 3. Group cards by TCG and Language and populate database
	groups := make(map[GroupKey][]string)

	fmt.Println("Populating database with real card metadata...")
	insertedCount := 0
	for filename, meta := range metadata {
		localPath := filepath.Join("test_cards", filename)
		if _, err := os.Stat(localPath); err != nil {
			// Skip if file doesn't exist on disk
			continue
		}

		cardID := extractRealCardID(filename)
		game := "Pokemon"
		if strings.Contains(filename, "OP") {
			game = "One Piece"
		}

		// Insert into postgres database
		_, err = db.DB.Exec(`
			INSERT INTO cards (id, name, set_name, game, language, image_url, price_usd, price_eur, last_updated)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, game = EXCLUDED.game, language = EXCLUDED.language`,
			cardID, meta.Name, game, game, meta.Lang, "", 0.0, 0.0)
		if err != nil {
			fmt.Printf("Error inserting card %s: %v\n", cardID, err)
		} else {
			insertedCount++
		}

		key := GroupKey{Game: game, Lang: meta.Lang}
		groups[key] = append(groups[key], localPath)
	}
	fmt.Printf("Successfully populated %d cards in the database.\n", insertedCount)

	fmt.Println("\nAvailable TCG & Language Groups on disk:")
	fmt.Println("----------------------------------------")
	for key, paths := range groups {
		fmt.Printf("  - TCG: %-15s | Language: %-8s | Count: %d\n", key.Game, key.Lang, len(paths))
	}
	fmt.Println("----------------------------------------")

	// 4. Run OCR testing on up to 15 cards for each group
	fmt.Println("\nStarting OCR Scans (max 15 cards per group)...")

	results := make(map[GroupKey]TestResult)

	for key, paths := range groups {
		fmt.Printf("\n>>> Testing Group [TCG: %s | Language: %s] (%d cards available)\n", key.Game, key.Lang, len(paths))

		limit := 15
		if len(paths) < limit {
			limit = len(paths)
		}

		passed := 0
		tested := 0

		// Fetch all cards for this TCG and language from the database
		var dbCards []models.Card
		rows, err := db.DB.Query("SELECT id, name FROM cards WHERE game = $1 AND language = $2", key.Game, key.Lang)
		if err == nil {
			for rows.Next() {
				var c models.Card
				if err := rows.Scan(&c.ID, &c.Name); err == nil {
					dbCards = append(dbCards, c)
				}
			}
			if closeErr := rows.Close(); closeErr != nil {
				fmt.Printf("Failed to close card query rows: %v\n", closeErr)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				fmt.Printf("Failed while reading card query rows: %v\n", rowsErr)
			}
		}

		// Convert language string to Tesseract language code
		tessLang := key.Lang
		if tessLang == "en" {
			tessLang = "eng"
		} else if tessLang == "fr" {
			tessLang = "fra"
		} else if tessLang == "de" {
			tessLang = "deu"
		} else if tessLang == "ja" {
			tessLang = "jpn"
		} else if tessLang == "ko" {
			tessLang = "kor"
		} else if tessLang == "zh-cn" || tessLang == "zhs" {
			tessLang = "chi_sim"
		} else if tessLang == "zh-tw" || tessLang == "zht" {
			tessLang = "chi_tra"
		}

		for i := 0; i < limit; i++ {
			path := paths[i]
			filename := filepath.Base(path)
			meta := metadata[filename]

			imgBytes, err := os.ReadFile(path) // #nosec G304 -- paths are built from entries found in the local fixture directory.
			if err != nil {
				fmt.Printf("  [ERROR] Failed to read %s: %v\n", path, err)
				continue
			}

			tested++

			startTime := time.Now()
			// Run Tesseract OCR scan (using dbCards loaded from DB)
			extractedText, detected, _, err := service.ProcessCardScan(imgBytes, dbCards, tessLang, llm)
			duration := time.Since(startTime)

			if err != nil {
				fmt.Printf("  [%d/%d] ❌ %s -> OCR Error: %v (%v)\n", i+1, limit, filename, err, duration)
				continue
			}

			// Validate matching
			expectedName := strings.ToLower(meta.Name)
			expectedID := strings.ToLower(extractRealCardID(filename))
			detectedLower := strings.ToLower(detected)
			extractedTextLower := strings.ToLower(extractedText)

			// Match is successful if:
			// 1. Detected matches expected ID exactly (resolves translated name issues and duplicate names!)
			// 2. Detected matches expected Name exactly
			// 3. Or expected name is found inside the extracted OCR text
			matched := detectedLower == expectedID ||
				detectedLower == expectedName ||
				strings.Contains(extractedTextLower, expectedName) ||
				(detectedLower != "unknown card" && strings.Contains(expectedName, detectedLower))

			if matched {
				passed++
				fmt.Printf("  [%d/%d] ✅ %s -> Matched: \"%s\" (ID: %s) (%v)\n", i+1, limit, filename, meta.Name, expectedID, duration)
			} else {
				snippet := strings.ReplaceAll(extractedTextLower, "\n", " ")
				if len(snippet) > 60 {
					snippet = snippet[:60] + "... "
				}
				fmt.Printf("  [%d/%d] ❌ %s -> Expected: \"%s\" (ID: %s) | Detected: \"%s\" | Got text: \"%s\" (%v)\n",
					i+1, limit, filename, meta.Name, expectedID, detected, snippet, duration)
			}
		}

		results[key] = TestResult{
			Total:  tested,
			Passed: passed,
		}
	}

	// 4. Print Summary Table
	fmt.Println("\n==================================================================================")
	fmt.Println("                                OCR SUMMARY TABLE")
	fmt.Println("==================================================================================")
	fmt.Printf("%-20s | %-12s | %-12s | %-12s | %-10s\n", "TCG (Game)", "Language", "Tested Cards", "Passed Cards", "Accuracy")
	fmt.Println("----------------------------------------------------------------------------------")

	totalTested := 0
	totalPassed := 0

	for key, res := range results {
		accuracy := 0.0
		if res.Total > 0 {
			accuracy = float64(res.Passed) / float64(res.Total) * 100
		}
		fmt.Printf("%-20s | %-12s | %-12d | %-12d | %.2f%%\n",
			key.Game, key.Lang, res.Total, res.Passed, accuracy)

		totalTested += res.Total
		totalPassed += res.Passed
	}
	fmt.Println("----------------------------------------------------------------------------------")

	overallAccuracy := 0.0
	if totalTested > 0 {
		overallAccuracy = float64(totalPassed) / float64(totalTested) * 100
	}
	fmt.Printf("%-20s | %-12s | %-12d | %-12d | %.2f%%\n",
		"OVERALL TOTAL", "ALL", totalTested, totalPassed, overallAccuracy)
	fmt.Println("==================================================================================")
}

func extractRealCardID(filename string) string {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	base = strings.TrimSuffix(base, ext)

	// List of prefixes to remove
	prefixes := []string{"eng_", "fra_", "deu_", "jpn_", "kor_", "chi_sim_", "chi_tra_", "test_"}
	for _, p := range prefixes {
		if strings.HasPrefix(base, p) {
			base = strings.TrimPrefix(base, p)
			break
		}
	}
	return base
}
