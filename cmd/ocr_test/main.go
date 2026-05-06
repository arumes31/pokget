package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"pokget/internal/service"
)

func main() {
	// 1. URLs for real TCG cards
	cardURLs := map[string]string{
		"Charizard VMAX": "https://tcgplayer-cdn.tcgplayer.com/product/219233_in_1000x1000.jpg",
		"Luffy ST01-001": "https://tcgplayer-cdn.tcgplayer.com/product/288228_in_1000x1000.jpg",
	}

	mockCards := []models.Card{
		{ID: "char-1", Name: "Charizard"},
		{ID: "luffy-1", Name: "Luffy"},
	}

	// Create debug directory
	_ = os.MkdirAll("static/img/debug", 0755)

	for name, url := range cardURLs {
		fmt.Printf("--- Testing OCR for: %s ---\n", name)
		
		// Download image
		// #nosec G107
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("Failed to download %s: %v\n", name, err)
			continue
		}
		imgBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Run OCR Preprocessing and Scan
		text, detected, processed, err := service.ProcessCardScan(imgBytes, mockCards, "eng")
		if err != nil {
			fmt.Printf("OCR Logic Error: %v\n", err)
			continue
		}

		fmt.Printf("Detected Name: %s\n", detected)
		fmt.Printf("Raw OCR Text (Sample): %.50s...\n", text)

		// Save processed image for visual inspection
		safeName := filepath.Join("static/img/debug", name+".jpg")
		if processed != nil {
			_ = os.WriteFile(safeName, processed, 0600)
			fmt.Printf("Processed image saved to: %s\n", safeName)
		}
		fmt.Println()
	}
}
