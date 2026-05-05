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
	return &LLMService{
		BaseURL:    url,
		Model:      "tinyllama", // Extremely fast on CPU
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *LLMService) FuzzyMatchCard(ocrText string, knownCards []models.Card) (string, error) {
	cardNames := []string{}
	for _, c := range knownCards {
		cardNames = append(cardNames, c.Name)
	}

	prompt := fmt.Sprintf(`The following text was extracted from a trading card using OCR and might have typos: "%s".
Which of these card names is the most likely match?
Known cards: %s.
Respond ONLY with the card name. If no match is found, respond with "Unknown Card".`, ocrText, strings.Join(cardNames, ", "))

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

	return cleanedMatch, nil
}
