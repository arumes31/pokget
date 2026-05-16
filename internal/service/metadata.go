// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/http"
	"pokget/internal/models"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

type MetadataClient interface {
	FetchCards(ctx context.Context, game string, lang string) ([]models.Card, error)
}

type TCGDexClient struct {
	BaseURL string
}

func NewTCGDexClient() *TCGDexClient {
	return &TCGDexClient{
		BaseURL: "https://api.tcgdex.net/v2",
	}
}

type tcgDexCard struct {
	ID    string `json:"id"`
	LocalID string `json:"localId"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

func (t *TCGDexClient) FetchCards(ctx context.Context, game string, lang string) ([]models.Card, error) {
	if strings.ToLower(game) != "pokemon" {
		return nil, nil // Only supports Pokemon for now
	}

	url := fmt.Sprintf("%s/%s/cards", t.BaseURL, lang)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TCGDex API returned status: %d", resp.StatusCode)
	}

	var rawCards []tcgDexCard
	if err := json.NewDecoder(resp.Body).Decode(&rawCards); err != nil {
		return nil, err
	}

	var cards []models.Card
	for _, rc := range rawCards {
		if rc.Image == "" {
			continue
		}
		cards = append(cards, models.Card{
			ID:       rc.ID,
			Name:     rc.Name,
			ImageURL: rc.Image + "/high.webp",
			Game:     "Pokemon",
			Language: lang,
		})
	}

	return cards, nil
}

type MetadataService struct {
	fingerprint *FingerprintService
}

func NewMetadataService(f *FingerprintService) *MetadataService {
	return &MetadataService{fingerprint: f}
}

func (s *MetadataService) ProcessCard(ctx context.Context, card models.Card) (*models.Card, error) {
	slog.Info("Metadata: Processing card", "id", card.ID, "name", card.Name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, card.ImageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to download image: status %d %s", resp.StatusCode, resp.Status)
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	hash, err := s.fingerprint.CalculateHash(img)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	card.Phash = &hash
	return &card, nil
}
