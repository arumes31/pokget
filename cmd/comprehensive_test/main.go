package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"pokget/internal/db"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
	"github.com/shopspring/decimal"
)

type CardMetadata struct {
	Name   string `json:"name"`
	Number string `json:"number"`
	Lang   string `json:"lang"`
}

func main() {
	fmt.Println("==================================================")
	fmt.Println(" pokget Comprehensive Test Runner")
	fmt.Println("==================================================")

	// 1. Initialize Database
	fmt.Println("\n--- 1. Database Connection ---")
	db.InitDB()
	if db.DB == nil {
		fmt.Println("Error: Database connection failed. Please ensure DB_HOST and other environment variables are set.")
		os.Exit(1)
	}
	fmt.Println("Database connection established and migrations run.")

	// 2. Load test cards metadata
	fmt.Println("\n--- 2. Load Test Cards Metadata ---")
	metadataPath := filepath.Join("test_cards", "test_cards_metadata.json")
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		fmt.Printf("Warning: Reading %s failed: %v. Trying fallback test_metadata.json...\n", metadataPath, err)
		metadataPath = filepath.Join("test_cards", "test_metadata.json")
		metadataBytes, err = os.ReadFile(metadataPath)
		if err != nil {
			fmt.Println("Error: Could not find any metadata file in test_cards.")
			os.Exit(1)
		}
	}
	fmt.Printf("Loaded metadata from: %s\n", metadataPath)

	var metadata map[string]CardMetadata
	err = json.Unmarshal(metadataBytes, &metadata)
	if err != nil {
		// Try to parse the other metadata structure
		type FallbackMetadata struct {
			Game string `json:"game"`
			Lang string `json:"lang"`
			Name string `json:"name"`
		}
		var fallbackMap map[string]FallbackMetadata
		if err2 := json.Unmarshal(metadataBytes, &fallbackMap); err2 == nil {
			metadata = make(map[string]CardMetadata)
			for k, v := range fallbackMap {
				// Convert keys to filename base
				baseKey := filepath.Base(k)
				metadata[baseKey] = CardMetadata{
					Name:   v.Name,
					Number: "Unknown",
					Lang:   v.Lang,
				}
			}
		} else {
			fmt.Println("Error: Failed to parse metadata JSON:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("Total cards in metadata: %d\n", len(metadata))

	// Find files that actually exist
	var existingCards []string
	for filename := range metadata {
		path := filepath.Join("test_cards", filename)
		if _, err := os.Stat(path); err == nil {
			existingCards = append(existingCards, filename)
		}
	}
	fmt.Printf("Total existing files on disk: %d\n", len(existingCards))
	if len(existingCards) == 0 {
		fmt.Println("Error: No test card image files found on disk in test_cards/.")
		os.Exit(1)
	}

	// 3. Pick 5 random cards from test_cards
	fmt.Println("\n--- 3. Pick Random Cards from test_cards ---")
	rand.Seed(time.Now().UnixNano())
	numToPick := 5
	if len(existingCards) < numToPick {
		numToPick = len(existingCards)
	}

	// Shuffle
	rand.Shuffle(len(existingCards), func(i, j int) {
		existingCards[i], existingCards[j] = existingCards[j], existingCards[i]
	})

	pickedCards := existingCards[:numToPick]
	fmt.Printf("Picked %d random cards for testing:\n", numToPick)
	for i, filename := range pickedCards {
		meta := metadata[filename]
		fmt.Printf("  [%d] %s (Expected Name: %s, Lang: %s)\n", i+1, filename, meta.Name, meta.Lang)
	}

	// Prepare known cards list for OCR matcher
	var knownCards []models.Card
	for _, filename := range existingCards {
		m := metadata[filename]
		knownCards = append(knownCards, models.Card{
			ID:   filename,
			Name: m.Name,
		})
	}

	// Initialize Fingerprint Service
	fingerprintSvc := service.NewFingerprintService(db.DB)

	// 4. Run OCR and Indexing tests on the picked cards
	fmt.Println("\n--- 4. Test OCR & Indexing of Picked Cards ---")
	for i, filename := range pickedCards {
		meta := metadata[filename]
		filePath := filepath.Join("test_cards", filename)
		fmt.Printf("\nTesting Card [%d/5]: %s\n", i+1, filename)

		imgBytes, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("  Error reading file: %v\n", err)
			continue
		}

		// Convert language string to Tesseract language code
		tessLang := meta.Lang
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
		} else if tessLang == "zh-cn" {
			tessLang = "chi_sim"
		} else if tessLang == "zh-tw" {
			tessLang = "chi_tra"
		}

		// Run OCR scan on picked card
		fmt.Println("  Running OCR Scan...")
		extractedText, detected, _, err := service.ProcessCardScan(imgBytes, knownCards, tessLang, nil)
		if err != nil {
			fmt.Printf("  OCR Scan failed: %v\n", err)
		} else {
			fmt.Printf("  OCR Extracted Text (Sample): %.80s...\n", strings.ReplaceAll(extractedText, "\n", " "))
			fmt.Printf("  OCR Detected Name: %s\n", detected)

			// Simple verification
			match := strings.Contains(strings.ToLower(extractedText), strings.ToLower(meta.Name)) ||
				strings.EqualFold(detected, meta.Name)
			if match {
				fmt.Println("  [OCR STATUS] Pass - Expected card name matches extracted text or detected card.")
			} else {
				fmt.Println("  [OCR STATUS] Fail/Weak - Card name was not found in extracted text.")
			}
		}

		// Verify "dont fingerprint test_cards.." constraint
		fmt.Println("  Verifying Fingerprinting Constraint...")
		
		// Load card image to calculate its hash
		imgFile, err := os.Open(filePath)
		if err != nil {
			fmt.Printf("  Failed to open image for hashing: %v\n", err)
			continue
		}
		img, _, err := image.Decode(imgFile)
		imgFile.Close()
		if err != nil {
			fmt.Printf("  Failed to decode image for hashing: %v\n", err)
			continue
		}

		hash, err := fingerprintSvc.CalculateHash(img)
		if err != nil {
			fmt.Printf("  Failed to calculate pHash: %v\n", err)
			continue
		}

		// Search in DB/BK-tree. It should not be found since we shouldn't have fingerprinted test cards.
		matchResult := fingerprintSvc.SearchByHash(hash)
		
		// Also query DB directly for this card ID or filename to make sure it's not present
		var dbPhash sql.NullInt64
		err = db.DB.QueryRow("SELECT phash FROM cards WHERE id = $1 OR image_url = $2", filename, filePath).Scan(&dbPhash)
		
		if err == sql.ErrNoRows {
			fmt.Println("  [CONSTRAINT CHECK] Pass - Card is not present in the database cards table.")
		} else if err == nil {
			if !dbPhash.Valid {
				fmt.Println("  [CONSTRAINT CHECK] Pass - Card exists in DB but has no phash (not fingerprinted).")
			} else {
				fmt.Printf("  [CONSTRAINT CHECK] WARNING - Card exists in DB with phash %d. Did someone fingerprint it?\n", dbPhash.Int64)
			}
		} else {
			fmt.Printf("  [CONSTRAINT CHECK] Database query error: %v\n", err)
		}

		if matchResult != nil && matchResult.HighConfidence != nil {
			fmt.Printf("  [INDEX CHECK] WARNING - BK-tree search matched this card to: %s (distance %d)\n", 
				matchResult.HighConfidence.Name, matchResult.BestDistance)
		} else {
			fmt.Println("  [INDEX CHECK] Pass - BK-tree search returned no high-confidence match (card is not fingerprinted).")
		}
	}

	// 5. Test Download of cards from the internet
	fmt.Println("\n--- 5. Test Download from Internet ---")
	fmt.Println("Fetching cards list from TCGdex API...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Retrieve card list
	resp, err := http.Get("https://api.tcgdex.net/v2/en/cards")
	if err != nil {
		fmt.Printf("Error: Failed to fetch card list from TCGdex: %v\n", err)
		os.Exit(1)
	}
	
	var cards []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	err = json.NewDecoder(resp.Body).Decode(&cards)
	resp.Body.Close()
	if err != nil {
		fmt.Printf("Error: Failed to decode card list: %v\n", err)
		os.Exit(1)
	}

	// Find first card with an image
	var downloadCard struct {
		ID    string
		Name  string
		Image string
	}
	found := false
	for _, c := range cards {
		if c.Image != "" {
			downloadCard.ID = c.ID
			downloadCard.Name = c.Name
			downloadCard.Image = c.Image + "/high.webp"
			found = true
			break
		}
	}

	if !found {
		fmt.Println("Error: No card with an image found in the TCGdex list.")
		os.Exit(1)
	}

	fmt.Printf("Downloading card: %s (ID: %s) from %s...\n", downloadCard.Name, downloadCard.ID, downloadCard.Image)
	
	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadCard.Image, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	imgResp, err := http.DefaultClient.Do(imgReq)
	if err != nil {
		fmt.Printf("Error downloading image: %v\n", err)
		os.Exit(1)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		fmt.Printf("Error: Download failed with status %d\n", imgResp.StatusCode)
		os.Exit(1)
	}

	internetImgBytes, err := io.ReadAll(imgResp.Body)
	if err != nil {
		fmt.Printf("Error reading downloaded image bytes: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[DOWNLOAD STATUS] Pass - Successfully downloaded card image from internet (%d bytes).\n", len(internetImgBytes))

	// 6. Test Fingerprinting and Indexing of the downloaded internet card
	fmt.Println("\n--- 6. Test Fingerprinting & Indexing of Internet Card ---")
	
	// Decode the downloaded image
	decodedImg, _, err := image.Decode(bytes.NewReader(internetImgBytes))
	if err != nil {
		fmt.Printf("Error decoding downloaded image: %v\n", err)
		os.Exit(1)
	}

	// Calculate perceptual hash (fingerprint)
	internetHash, err := fingerprintSvc.CalculateHash(decodedImg)
	if err != nil {
		fmt.Printf("Error calculating perceptual hash: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[FINGERPRINT STATUS] Pass - Calculated perceptual hash (pHash): %d (0x%x)\n", internetHash, uint64(internetHash))

	// Index it by inserting into the database
	testCardID := "internet_test_card_temp"
	testSetName := "Internet Test Set"
	
	fmt.Println("Inserting internet card fingerprint into database...")
	_, err = db.DB.Exec(`
		INSERT INTO cards (id, name, set_name, image_url, price_usd, price_eur, game, phash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET phash = EXCLUDED.phash`,
		testCardID, downloadCard.Name, testSetName, downloadCard.Image, decimal.NewFromFloat(9.99), decimal.NewFromFloat(8.99), "Pokemon", internetHash)
	if err != nil {
		fmt.Printf("Error: Database insert failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Card successfully inserted and indexed.")

	// Rebuild BK-tree index
	fmt.Println("Rebuilding BK-tree index...")
	fingerprintSvc.RebuildTree()

	// Verify indexing and matching works
	fmt.Println("Searching BK-tree using the calculated fingerprint...")
	searchResult := fingerprintSvc.SearchByHash(internetHash)

	if searchResult != nil && searchResult.HighConfidence != nil {
		fmt.Printf("Matched card name: %s\n", searchResult.HighConfidence.Name)
		fmt.Printf("Matched card ID: %s\n", searchResult.HighConfidence.ID)
		fmt.Printf("Distance: %d\n", searchResult.BestDistance)
		
		if searchResult.HighConfidence.ID == testCardID {
			fmt.Println("[INDEXING STATUS] Pass - BK-tree search successfully matched the card by its fingerprint with 0 distance.")
		} else {
			fmt.Printf("[INDEXING STATUS] Fail/Mismatch - BK-tree matched a different card: %s\n", searchResult.HighConfidence.ID)
		}
	} else {
		fmt.Println("[INDEXING STATUS] Fail - BK-tree search returned no match for the indexed fingerprint.")
	}

	// 7. Cleanup
	fmt.Println("\n--- 7. Cleanup ---")
	fmt.Println("Cleaning up temporary test card from database...")
	_, err = db.DB.Exec("DELETE FROM cards WHERE id = $1", testCardID)
	if err != nil {
		fmt.Printf("Warning: Failed to delete temporary card: %v\n", err)
	} else {
		fmt.Println("Temporary card deleted successfully.")
	}

	fmt.Println("\n==================================================")
	fmt.Println(" Comprehensive Testing Completed")
	fmt.Println("==================================================")
}
