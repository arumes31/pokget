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
	"bytes"
	"encoding/json"
	"fmt"
	"pokget/internal/models"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type LLMClient interface {
	FuzzyMatchCard(ocrText string, knownCards []models.Card) (string, error)
	GenerateBinderName(cards []models.Card) (string, error)
}

type LLMService struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

func NewLLMService() *LLMService {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "pokget_ollama"
	}
	url := fmt.Sprintf("http://%s:11434", host)
	svc := &LLMService{
		BaseURL:    url,
		Model:      "tinyllama", // Extremely fast on CPU
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
	go svc.AutoSetup()
	return svc
}

func (s *LLMService) AutoSetup() {
	slog.Info("LLM: Auto-setup started")

	// 1. Check if model exists
	resp, err := s.HTTPClient.Get(s.BaseURL + "/api/tags")
	if err != nil {
		slog.Error("LLM: Failed to check models", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("LLM: API returned error on tags", "status", resp.StatusCode)
		return
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		slog.Error("LLM: Failed to decode tags response", "error", err)
		return
	}

	exists := false
	for _, m := range tagsResp.Models {
		if strings.HasPrefix(m.Name, s.Model) {
			exists = true
			break
		}
	}

	if exists {
		slog.Info("LLM: Model already exists", "model", s.Model)
		return
	}

	// 2. Pull model if not exists
	slog.Info("LLM: Model not found, pulling...", "model", s.Model)
	payload := map[string]interface{}{
		"model":  s.Model,
		"stream": false,
	}
	jsonData, _ := json.Marshal(payload)

	resp, err = s.HTTPClient.Post(s.BaseURL+"/api/pull", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		slog.Error("LLM: Failed to pull model", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("LLM: Pull API returned error", "status", resp.StatusCode, "body", string(body))
		return
	}

	slog.Info("LLM: Model pulled successfully", "model", s.Model)
}

func (s *LLMService) FuzzyMatchCard(ocrText string, knownCards []models.Card) (string, error) {
	cardDetails := []string{}
	for _, c := range knownCards {
		cardDetails = append(cardDetails, fmt.Sprintf("%s (ID/Number: %s)", c.Name, c.ID))
	}

	prompt := fmt.Sprintf(`The following text was extracted from a trading card using OCR and might have typos: "%s".
Which of these cards is the most likely match based on the text? Look carefully for card names or small card numbers (like 50/50, 19/122, or IDs like swsh45-19) that match the known cards.
Known cards: %s.
Respond ONLY with the exact ID/Number of the matching card from the known cards list (e.g., "swsh45-19"). Do not include the name in your response. If no match is found, respond with "Unknown Card".`, ocrText, strings.Join(cardDetails, ", "))

	payload := map[string]interface{}{
		"model":  s.Model,
		"prompt": prompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := s.HTTPClient.Post(s.BaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("LLM API error", "status", resp.StatusCode, "body", string(body))
		return "", fmt.Errorf("LLM API returned non-OK status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read LLM response body: %w", err)
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal LLM response: %w", err)
	}

	cleanedMatch := strings.TrimSpace(result.Response)
	slog.Info("LLM Fallback Result", "raw", result.Response, "cleaned", cleanedMatch)

	// Fallback for conversational models: check if the response contains any known card ID
	for _, c := range knownCards {
		if strings.Contains(cleanedMatch, c.ID) {
			slog.Info("LLM: Extracted card ID from conversational response", "id", c.ID)
			return c.ID, nil
		}
	}

	return cleanedMatch, nil
}
func (s *LLMService) GenerateBinderName(cards []models.Card) (string, error) {
	if len(cards) == 0 {
		return "New Empty Binder", nil
	}

	cardNames := []string{}
	for i, c := range cards {
		cardNames = append(cardNames, c.Name)
		if i > 20 {
			break // Don't overwhelm the context
		}
	}

	prompt := fmt.Sprintf(`Based on the following cards in a binder, suggest a single, creative, and premium-sounding name for the binder: %s.
Respond ONLY with the name, no quotes or explanations.`, strings.Join(cardNames, ", "))

	payload := map[string]interface{}{
		"model":  s.Model,
		"prompt": prompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := s.HTTPClient.Post(s.BaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Response), nil
}
