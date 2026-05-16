// Copyright (c) 2026 arumes31
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"time"
)

type TCGdexCard struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

func main() {
	languages := []string{"en", "fr", "de", "jp"}
	limit := 50

	results := make(map[string]int)

	for _, lang := range languages {
		fmt.Printf("\n--- Testing Language: %s ---\n", lang)
		
		// 1. Fetch card list from TCGdex
		// Using a specific set to ensure we get valid images quickly, e.g. "swsh1" (Sword & Shield Base)
		url := fmt.Sprintf("https://api.tcgdex.net/v2/%s/sets/swsh1", lang)
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Failed to fetch set for %s: %v", lang, err)
			continue
		}
		
		var setData struct {
			Cards []TCGdexCard `json:"cards"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&setData); err != nil {
			log.Printf("Failed to decode set for %s: %v", lang, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		count := 0
		correct := 0

		for _, card := range setData.Cards {
			if count >= limit {
				break
			}
			if card.Image == "" {
				continue
			}

			// 2. Download Image
			imgURL := card.Image + "/high.png" // high res
			imgResp, err := client.Get(imgURL)
			if err != nil {
				continue
			}
			imgBytes, _ := io.ReadAll(imgResp.Body)
			imgResp.Body.Close()

			// 3. Prepare Mock DB Cards (for trigram/LLM matching)
			mockCards := []models.Card{
				{ID: card.ID, Name: card.Name},
			}

			// 4. Run Scan
			// Tesseract language codes: eng, fra, deu, jpn
			tessLang := "eng"
			switch lang {
			case "fr": tessLang = "fra"
			case "de": tessLang = "deu"
			case "jp": tessLang = "jpn"
			}

			_, detected, processed, err := service.ProcessCardScan(imgBytes, mockCards, tessLang, nil)
			if err != nil {
				fmt.Printf("Error scanning %s: %v\n", card.Name, err)
				continue
			}

			// Normalize names for comparison
			match := strings.EqualFold(strings.TrimSpace(detected), strings.TrimSpace(card.Name))
			if match {
				correct++
				fmt.Printf("✅ %s\n", card.Name)
			} else {
				fmt.Printf("❌ %s (Detected: %s)\n", card.Name, detected)
				// Save failure for analysis
				debugDir := filepath.Join("static/img/debug/multilang", lang)
				_ = os.MkdirAll(debugDir, 0750)
				_ = os.WriteFile(filepath.Join(debugDir, card.Name+".jpg"), processed, 0600)
			}

			count++
			time.Sleep(100 * time.Millisecond) // Be kind to API
		}

		fmt.Printf("Result for %s: %d/%d (%.1f%%)\n", lang, correct, count, float64(correct)/float64(count)*100)
		results[lang] = correct
	}

	fmt.Println("\n--- FINAL RESULTS ---")
	for lang, correct := range results {
		fmt.Printf("%s: %d/%d\n", lang, correct, limit)
	}
}
nge results {
		fmt.Printf("%s: %d/%d\n", lang, correct, limit)
	}
}
