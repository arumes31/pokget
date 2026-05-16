package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"pokget/internal/db"
	"pokget/internal/service"
	"strings"

	_ "golang.org/x/image/webp"
	"github.com/shopspring/decimal"
)

type MetadataInfo struct {
	Game string `json:"game"`
	Lang string `json:"lang"`
	Name string `json:"name"`
}

func main() {
	// 1. Load metadata
	data, err := os.ReadFile("test_cards/test_metadata.json")
	if err != nil {
		log.Fatalf("Failed to read metadata: %v", err)
	}

	var metadata map[string]MetadataInfo
	if err := json.Unmarshal(data, &metadata); err != nil {
		log.Fatalf("Failed to unmarshal metadata: %v", err)
	}

	// 2. Validate DB Environment Variables
	requiredEnvVars := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}
	var missing []string
	for _, env := range requiredEnvVars {
		if os.Getenv(env) == "" {
			missing = append(missing, env)
		}
	}

	if len(missing) > 0 {
		log.Fatalf("Error: Missing required database environment variables: %v. Please set them before running this script (e.g. via export or .env).", missing)
	}

	// 3. Init DB
	db.InitDB()

	if db.DB == nil {
		log.Fatal("DB connection failed")
	}

	fingerprint := service.NewFingerprintService(db.DB)

	fmt.Printf("Processing %d cards for fingerprinting...\n", len(metadata))

	for path, info := range metadata {
		// Replace forward slashes with system path separator if needed, but they are normalized to / in the downloader
		localPath := strings.ReplaceAll(path, "/", string(os.PathSeparator))
		
		imgFile, err := os.Open(localPath)
		if err != nil {
			fmt.Printf("⚠️ Skip %s: %v\n", localPath, err)
			continue
		}
		
		img, _, err := image.Decode(imgFile)
		imgFile.Close()
		if err != nil {
			fmt.Printf("⚠️ Skip %s: decode error %v\n", localPath, err)
			continue
		}

		hash, err := fingerprint.CalculateHash(img)
		if err != nil {
			fmt.Printf("⚠️ Skip %s: hash error %v\n", localPath, err)
			continue
		}

		// Insert/Update card in DB
		// Use a unique ID based on path/name
		cardID := "test_" + strings.ReplaceAll(strings.ToLower(info.Name), " ", "_") + "_" + info.Lang
		// Handle duplicate IDs (common with generic names from DDG)
		if strings.Contains(info.Name, "Card") {
			cardID = "test_" + strings.ReplaceAll(path, "/", "_")
		}

		_, err = db.DB.Exec(`
			INSERT INTO cards (id, name, set_name, image_url, price_usd, price_eur, game, phash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET phash = EXCLUDED.phash`,
			cardID, info.Name, info.Game+" Test Set", path, decimal.NewFromFloat(1.0), decimal.NewFromFloat(0.9), info.Game, hash)
		
		if err != nil {
			fmt.Printf("❌ Failed DB insert for %s: %v\n", info.Name, err)
		} else {
			fmt.Printf("✅ Fingerprinted: %s (%s)\n", info.Name, info.Game)
		}
	}

	fmt.Println("Done fingerprinting.")
}
