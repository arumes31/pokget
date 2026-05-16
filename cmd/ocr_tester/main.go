package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"pokget/internal/models"
	"pokget/internal/service"
)

type CardMetadata struct {
	Name   string `json:"name"`
	Number string `json:"number"`
	Lang   string `json:"lang"`
}

func main() {
	metadataPath := filepath.Join("test_cards", "test_cards_metadata.json")
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		fmt.Println("Error reading metadata file:", err)
		fmt.Println("Please run prepare_test_cards.py first.")
		return
	}

	var metadata map[string]CardMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	if err != nil {
		fmt.Println("Error parsing metadata JSON:", err)
		return
	}

	var knownCards []models.Card
	for _, m := range metadata {
		knownCards = append(knownCards, models.Card{
			Name: m.Name,
			ID:   m.Number,
		})
	}
	llm := service.NewLLMService()

	files, err := os.ReadDir("test_cards")
	if err != nil {
		fmt.Println("Error reading test_cards dir:", err)
		return
	}

	successCount := 0
	totalCount := 0

	fmt.Println("Starting Multi-Language OCR Test with Metadata...")
	fmt.Println("==================================================")

	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), "test_OP01-") {
			continue
		}
		
		meta, ok := metadata[f.Name()]
		if !ok {
			// Skip files not in metadata (like old test cards if any)
			continue
		}

		totalCount++
		filePath := filepath.Join("test_cards", f.Name())
		imgBytes, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", f.Name(), err)
			continue
		}

		// Use specific language from metadata
		lang := meta.Lang

		fmt.Printf("Processing %s (%s)...\n", f.Name(), lang)
		os.Stdout.Sync()

		// Discard standard output/log to avoid cluttering the test results
		origStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		extractedText, _, _, err := service.ProcessCardScan(imgBytes, knownCards, lang, llm)
		
		w.Close()
		os.Stdout = origStdout
		io.ReadAll(r)

		if err != nil {
			fmt.Printf("[FAIL] %s - Error: %v\n", f.Name(), err)
			continue
		}

		extractedTextLower := strings.ToLower(extractedText)
		nameLower := strings.ToLower(meta.Name)
		number := meta.Number

		// Flexible validation:
		// 1. Check if name is in text
		// 2. Check if number is in text
		
		matchedName := strings.Contains(extractedTextLower, nameLower)
		matchedNumber := strings.Contains(extractedTextLower, strings.ToLower(number))

		// Also check without leading zeros for number
		cleanNumber := strings.TrimLeft(number, "0")
		if cleanNumber == "" {
			cleanNumber = "0"
		}
		matchedCleanNumber := strings.Contains(extractedTextLower, strings.ToLower(cleanNumber))

		success := matchedName || matchedNumber || matchedCleanNumber

		if success {
			successCount++
			fmt.Printf("[PASS] %s - Matched: Name=%t, Num=%t (CleanNum=%t)\n", f.Name(), matchedName, matchedNumber, matchedCleanNumber)
		} else {
			snippet := strings.ReplaceAll(extractedTextLower, "\n", " ")
			if len(snippet) > 100 {
				snippet = snippet[:100]
			}
			fmt.Printf("[FAIL] %s - Expected: %s | %s. Got snippet: %s\n", f.Name(), meta.Name, meta.Number, snippet)
		}
	}

	fmt.Println("==================================================")
	fmt.Printf("Results: %d/%d passed.\n", successCount, totalCount)
	
	if totalCount > 0 {
		accuracy := float64(successCount) / float64(totalCount) * 100
		fmt.Printf("Accuracy: %.2f%%\n", accuracy)
	}
}
