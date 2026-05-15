package main

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
)

func downloadImage(url string) ([]byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

type TestCard struct {
	DisplayName string
	Lang        string
	ImageURL    string
	SearchName  string // The name we expect to find in the database
}

func main() {
	testCards := []TestCard{
		{
			DisplayName: "English Celebi (Celebi)",
			Lang:        "eng",
			ImageURL:    "https://assets.tcgdex.net/en/swsh/swsh1/1/low.png",
			SearchName:  "Celebi",
		},
		{
			DisplayName: "Japanese Normalization Test (Celebi)",
			Lang:        "jpn",
			ImageURL:    "https://assets.tcgdex.net/en/swsh/swsh1/1/low.png",
			SearchName:  "Celebi",
		},
	}

	mockDB := []models.Card{
		{Name: "Celebi", Set: "Sword & Shield"},
	}

	fmt.Printf("🌍 Starting Multi-Language OCR Detection Test\n")
	fmt.Printf("----------------------------------------------\n")

	_ = os.MkdirAll("static/img/debug/multilang", 0750)

	for _, tc := range testCards {
		fmt.Printf("🧪 Testing: %s [%s]\n", tc.DisplayName, tc.Lang)
		
		// 1. Download image
		imgBytes, err := downloadImage(tc.ImageURL)
		if err != nil {
			fmt.Printf("   ❌ Download Failed: %v\n", err)
			continue
		}

		// 2. Run OCR Detection
		// We pass the specific language code to Tesseract/Matching logic
		text, detected, processed, err := service.ProcessCardScan(imgBytes, mockDB, tc.Lang, nil)
		if err != nil {
			fmt.Printf("   ❌ OCR Error: %v\n", err)
			continue
		}

		// 3. Evaluate Result
		status := "✅ MATCHED"
		if !strings.Contains(strings.ToLower(detected), strings.ToLower(tc.SearchName)) {
			status = "❌ MISMATCH"
		}

		fmt.Printf("   Result: %s\n", status)
		fmt.Printf("   Detected: %s\n", detected)
		fmt.Printf("   OCR Text (Sample): %.60s...\n", text)

		// Save processed image
		safeName := filepath.Join("static/img/debug/multilang", strings.ReplaceAll(tc.DisplayName, " ", "_")+".jpg")
		_ = os.WriteFile(safeName, processed, 0600)
		fmt.Println()
	}

	fmt.Printf("----------------------------------------------\n")
	fmt.Printf("📊 Summary: Test completed. Check static/img/debug/multilang for processed vision output.\n")
}
