package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/service"
)

func main() {
	path := flag.String("image", "", "image path")
	game := flag.String("game", "pokemon", "game hint")
	language := flag.String("language", "English", "language hint")
	flag.Parse()

	payload, err := os.ReadFile(*path) // #nosec G304 - operator-supplied diagnostic image
	if err != nil {
		panic(err)
	}
	variants, err := service.ImageVariantsForDiagnostics(context.Background(), payload)
	if err != nil {
		panic(err)
	}
	client, err := claudedetect.New(claudedetect.Config{
		APIKey:        os.Getenv("CLAUDE_API_KEY"),
		Model:         os.Getenv("CLAUDE_MODEL"),
		Timeout:       60 * time.Second,
		PromptCaching: true,
	})
	if err != nil {
		panic(err)
	}
	info, err := client.DetectImages(context.Background(), variants, *game, *language)
	if err != nil {
		panic(err)
	}
	encoded, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
}
