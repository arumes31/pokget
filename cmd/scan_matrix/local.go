package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"pokget/internal/models"
)

func printJSON(payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

// runLocalImage sends one local photo (a genuine camera image, not a catalog
// render) through the live endpoint. Labels are optional: without them the row
// only reports what the server returned.
func runLocalImage(live liveConfig, imagePath, game, language string, expected models.Card, asJSON bool, timeout time.Duration) error {
	if strings.TrimSpace(live.BaseURL) == "" {
		return errors.New("-image requires -base-url")
	}
	if strings.TrimSpace(game) == "" || strings.TrimSpace(language) == "" {
		return errors.New("-image requires -game and -language")
	}
	payload, err := os.ReadFile(imagePath) // #nosec G304 - operator-supplied fixture path
	if err != nil {
		return fmt.Errorf("read image: %w", err)
	}
	session, err := newLiveSession(context.Background(), live)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := session.scan(ctx, game, language, payload)
	if err != nil {
		return err
	}
	sample := sampleCard{Card: expected}
	row := resultRow{
		Game:           game,
		Language:       language,
		HintLanguage:   language,
		ExpectedID:     expected.ID,
		ExpectedName:   expected.Name,
		ExpectedNumber: expected.CollectorNumber,
	}
	fillLiveRow(&row, result, sample)
	if asJSON {
		return printJSON(map[string]any{"rows": []resultRow{row}})
	}
	printRow(row)
	return nil
}
