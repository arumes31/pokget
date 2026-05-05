package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gettos/internal/models"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type LLMService struct {
	BaseURL string
	Model   string
}

func NewLLMService() *LLMService {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = "http://ollama:11434"
	}
	return &LLMService{
		BaseURL: url,
		Model:   "tinyllama", // Extremely fast on CPU
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(s.BaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
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
