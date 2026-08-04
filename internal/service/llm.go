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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"pokget/internal/models"
	"sort"
	"strings"
	"time"
)

// LLMClient defines the interface for LLM-based card matching.
type LLMClient interface {
	FuzzyMatchCard(ocrText string, knownCards []models.Card) (string, error)
	GenerateBinderName(cards []models.Card) (string, error)
}

// LLMService provides LLM-based card identification via Ollama.
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
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (s *LLMService) AutoSetup() {
	s.AutoSetupContext(context.Background())
}

// AutoSetupContext ensures the configured Ollama model is available. Callers
// choose when to start it so service configuration is complete before a
// background goroutine reads it, and shutdown can cancel in-flight requests.
func (s *LLMService) AutoSetupContext(ctx context.Context) {
	slog.Info("LLM: Auto-setup started")
	baseURL, model, client := s.BaseURL, s.Model, s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	// 1. Check if model exists
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		slog.Error("LLM: Failed to create model check request", "error", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
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
		if strings.HasPrefix(m.Name, model) {
			exists = true
			break
		}
	}

	if exists {
		slog.Info("LLM: Model already exists", "model", model)
		return
	}

	// 2. Pull model if not exists
	slog.Info("LLM: Model not found, pulling...", "model", model)
	payload := map[string]interface{}{
		"model":  model,
		"stream": false,
	}
	jsonData, _ := json.Marshal(payload)

	pullClient := &http.Client{Timeout: 15 * time.Minute} // Pulling models can take a long time
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewBuffer(jsonData))
	if err != nil {
		slog.Error("LLM: Failed to create model pull request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = pullClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Error("LLM: Failed to pull model", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("LLM: Pull API returned error", "status", resp.StatusCode, "body", string(body))
		return
	}

	slog.Info("LLM: Model pulled successfully", "model", model)
}

func (s *LLMService) queryLLM(prompt string) (string, error) {
	return s.queryLLMContext(context.Background(), prompt)
}

func (s *LLMService) queryLLMContext(ctx context.Context, prompt string) (string, error) {
	payload := map[string]interface{}{
		"model":  s.Model,
		"prompt": prompt,
		"stream": false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/api/generate", bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create LLM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTPClient.Do(req)
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

	return result.Response, nil
}

// LLMCardResponse represents a validated LLM card identification response (SCAN-15).
type LLMCardResponse struct {
	CardName   string  `json:"card_name"`
	CardID     string  `json:"card_id,omitempty"`
	Confidence float64 `json:"confidence"`
}

// sanitizeOCRText removes potential prompt injection patterns from OCR text
func sanitizeOCRText(text string) string {
	// Limit length to prevent excessively long prompts
	if len(text) > 500 {
		text = text[:500]
	}
	// Remove common prompt-breaking patterns
	replacements := []struct{ old, new string }{
		{"Ignore", ""},
		{"ignore", ""},
		{"IGNORE", ""},
		{"Disregard", ""},
		{"disregard", ""},
		{"DISREGARD", ""},
		{"System:", ""},
		{"system:", ""},
		{"Assistant:", ""},
		{"assistant:", ""},
		{"<|", ""},
		{"|>", ""},
	}
	for _, r := range replacements {
		text = strings.ReplaceAll(text, r.old, r.new)
	}
	// Remove newlines that could break the prompt structure
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.TrimSpace(text)
}

// FuzzyMatchCard sends OCR text to the LLM for fuzzy matching against known cards.
// SCAN-08: Only sends a shortlist of candidate cards instead of all 15K+ names.
func (s *LLMService) FuzzyMatchCard(ocrText string, knownCards []models.Card) (string, error) {
	return s.FuzzyMatchCardContext(context.Background(), ocrText, knownCards)
}

// FuzzyMatchCardContext matches OCR text while honoring request cancellation.
func (s *LLMService) FuzzyMatchCardContext(ctx context.Context, ocrText string, knownCards []models.Card) (string, error) {
	// SCAN-08: Create a shortlist of top candidates instead of sending all cards
	shortlist := buildShortlist(ocrText, knownCards, 30)

	cardNames := make([]string, 0, len(shortlist))
	for _, c := range shortlist {
		cardNames = append(cardNames, c.Name)
	}

	var cardListStr string
	if len(cardNames) > 0 {
		cardListStr = strings.Join(cardNames, ", ")
	} else if len(knownCards) > 0 {
		// Fallback: if no shortlist could be built, send a limited set
		limit := len(knownCards)
		if limit > 50 {
			limit = 50
		}
		fallbackNames := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			fallbackNames = append(fallbackNames, knownCards[i].Name)
		}
		cardListStr = strings.Join(fallbackNames, ", ")
	}

	sanitizedOCR := sanitizeOCRText(ocrText)
	prompt := fmt.Sprintf(`The following text was extracted from a trading card using OCR and might have typos: "%s".
Which of these card names is the most likely match?
Known cards: %s.
Respond ONLY with the card name. If no match is found, respond with "Unknown Card".`, sanitizedOCR, cardListStr)

	response, err := s.queryLLMContext(ctx, prompt)
	if err != nil {
		return "", err
	}

	cleanedMatch := strings.TrimSpace(response)
	slog.Info("LLM Fallback Result", "raw", response, "cleaned", cleanedMatch)

	// Fallback for conversational models: check if the response contains any known card name or ID
	cleanedMatchLower := strings.ToLower(cleanedMatch)
	for _, c := range shortlist {
		if (c.ID != "" && strings.Contains(cleanedMatchLower, strings.ToLower(c.ID))) ||
			(c.Name != "" && strings.Contains(cleanedMatchLower, strings.ToLower(c.Name))) {
			slog.Info("LLM: Extracted card from conversational response", "id", c.ID, "name", c.Name)
			return c.Name, nil
		}
	}

	// Also check against all known cards (in case shortlist missed it)
	for _, c := range knownCards {
		if (c.ID != "" && strings.Contains(cleanedMatchLower, strings.ToLower(c.ID))) ||
			(c.Name != "" && strings.Contains(cleanedMatchLower, strings.ToLower(c.Name))) {
			slog.Info("LLM: Extracted card from full card list", "id", c.ID, "name", c.Name)
			return c.Name, nil
		}
	}

	return cleanedMatch, nil
}

// FuzzyMatchCardWithValidation sends OCR text to the LLM and validates the
// response format and card existence (SCAN-15).
func (s *LLMService) FuzzyMatchCardWithValidation(ocrText string, knownCards []models.Card) (*LLMCardResponse, error) {
	return s.FuzzyMatchCardWithValidationContext(context.Background(), ocrText, knownCards)
}

// FuzzyMatchCardWithValidationContext validates an LLM match while honoring cancellation.
func (s *LLMService) FuzzyMatchCardWithValidationContext(ctx context.Context, ocrText string, knownCards []models.Card) (*LLMCardResponse, error) {
	shortlist := buildShortlist(ocrText, knownCards, 30)

	cardNames := make([]string, 0, len(shortlist))
	for _, c := range shortlist {
		cardNames = append(cardNames, c.Name)
	}

	sanitizedOCR := sanitizeOCRText(ocrText)
	prompt := fmt.Sprintf(`The following text was extracted from a trading card using OCR and might have typos: "%s".
Which of these card names is the most likely match?
Known cards: %s.
Respond in JSON format: {"card_name": "the card name", "confidence": 0.9}
If no match is found, respond with: {"card_name": "Unknown Card", "confidence": 0.0}`, sanitizedOCR, strings.Join(cardNames, ", "))

	response, err := s.queryLLMContext(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM query failed: %w", err)
	}

	// SCAN-15: Validate LLM response format
	cleaned := strings.TrimSpace(response)

	// Try to extract JSON from the response (LLM may add extra text)
	jsonStart := strings.Index(cleaned, "{")
	jsonEnd := strings.LastIndex(cleaned, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		// Fallback: try plain text matching
		return s.validatePlainTextResponse(cleaned, knownCards, shortlist)
	}

	jsonStr := cleaned[jsonStart : jsonEnd+1]
	var llmResp LLMCardResponse
	if err := json.Unmarshal([]byte(jsonStr), &llmResp); err != nil {
		slog.Warn("LLM: Failed to parse JSON response, falling back to text matching", "error", err, "response", cleaned)
		return s.validatePlainTextResponse(cleaned, knownCards, shortlist)
	}

	// Validate required fields (SCAN-15)
	if llmResp.CardName == "" {
		return nil, fmt.Errorf("LLM response missing card_name field")
	}

	// Validate confidence range (SCAN-15)
	if llmResp.Confidence < 0 {
		llmResp.Confidence = 0
	}
	if llmResp.Confidence > 1 {
		llmResp.Confidence = 1
	}

	// Verify the card exists in the database (SCAN-15)
	if llmResp.CardName != "Unknown Card" {
		found := false
		for _, c := range knownCards {
			if strings.EqualFold(c.Name, llmResp.CardName) || c.ID == llmResp.CardID {
				llmResp.CardName = c.Name // Use exact name from DB
				llmResp.CardID = c.ID
				found = true
				break
			}
		}
		if !found {
			slog.Warn("LLM: Response card not found in database", "card", llmResp.CardName)
			return &LLMCardResponse{
				CardName:   "Unknown Card",
				Confidence: 0,
			}, nil
		}
	}

	return &llmResp, nil
}

// validatePlainTextResponse handles non-JSON LLM responses with validation (SCAN-15).
func (s *LLMService) validatePlainTextResponse(text string, allCards []models.Card, shortlist []models.Card) (*LLMCardResponse, error) {
	cleaned := strings.TrimSpace(text)
	cleanedLower := strings.ToLower(cleaned)

	// Check against shortlist first
	for _, c := range shortlist {
		if (c.ID != "" && strings.Contains(cleanedLower, strings.ToLower(c.ID))) ||
			(c.Name != "" && strings.Contains(cleanedLower, strings.ToLower(c.Name))) {
			return &LLMCardResponse{
				CardName:   c.Name,
				CardID:     c.ID,
				Confidence: 0.7, // Lower confidence for non-JSON response
			}, nil
		}
	}

	// Check against all cards
	for _, c := range allCards {
		if (c.ID != "" && strings.Contains(cleanedLower, strings.ToLower(c.ID))) ||
			(c.Name != "" && strings.Contains(cleanedLower, strings.ToLower(c.Name))) {
			return &LLMCardResponse{
				CardName:   c.Name,
				CardID:     c.ID,
				Confidence: 0.5, // Even lower confidence for non-shortlist match
			}, nil
		}
	}

	return &LLMCardResponse{
		CardName:   cleaned,
		Confidence: 0.1,
	}, nil
}

// buildShortlist creates a shortlist of candidate cards for LLM disambiguation (SCAN-08).
// It scores cards based on card ID matching, exact name substring matching, CJK-aware normalized matching, and word overlap ratio.
func buildShortlist(ocrText string, cards []models.Card, maxCandidates int) []models.Card {
	if len(cards) == 0 {
		return nil
	}

	// If there are few cards, just return them all (up to maxCandidates)
	if len(cards) <= maxCandidates {
		return cards
	}

	type scoredCard struct {
		card  models.Card
		score int
	}

	scored := make([]scoredCard, 0, len(cards))
	ocrLower := strings.ToLower(ocrText)

	for _, c := range cards {
		nameLower := strings.ToLower(c.Name)
		idLower := strings.ToLower(c.ID)

		score := 0

		// 1. Match card ID (very strong signal)
		if idLower != "" && len(idLower) >= 3 {
			if strings.Contains(ocrLower, idLower) {
				score += 500
			} else {
				// Normalize O vs 0 and check
				normOCR := strings.ReplaceAll(ocrLower, "0", "o")
				normID := strings.ReplaceAll(idLower, "0", "o")
				if strings.Contains(normOCR, normID) {
					score += 400
				}
			}
		}

		// 2. Match card name exactly as substring
		if strings.Contains(ocrLower, nameLower) {
			score += 300 + len(nameLower)
		} else {
			// Check CJK normalized match (remove spaces/newlines)
			ocrNoSpaces := strings.ReplaceAll(ocrLower, " ", "")
			ocrNoSpaces = strings.ReplaceAll(ocrNoSpaces, "\n", "")
			nameNoSpaces := strings.ReplaceAll(nameLower, " ", "")
			if nameNoSpaces != "" && strings.Contains(ocrNoSpaces, nameNoSpaces) {
				score += 250 + len(nameNoSpaces)
			} else {
				// 3. Word overlap score
				nameWords := strings.Fields(nameLower)
				matchedWords := 0
				for _, w := range nameWords {
					if len(w) >= 2 && strings.Contains(ocrLower, w) {
						matchedWords++
					}
				}
				if len(nameWords) > 0 {
					ratio := float64(matchedWords) / float64(len(nameWords))
					if ratio > 0.3 {
						score += int(ratio * 150)
					}
				}
			}
		}

		// 4. Tie-breaker: use negative Levenshtein distance as a penalization,
		// so that closer matches are preferred among equal scores.
		dist := levenshtein(ocrLower, nameLower)
		finalScore := score*100 - dist

		scored = append(scored, scoredCard{card: c, score: finalScore})
	}

	// Sort by score descending (highest score first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	limit := maxCandidates
	if limit > len(scored) {
		limit = len(scored)
	}

	result := make([]models.Card, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, scored[i].card)
	}

	return result
}

func (s *LLMService) GenerateBinderName(cards []models.Card) (string, error) {
	if len(cards) == 0 {
		return "New Empty Binder", nil
	}

	limit := len(cards)
	if limit > 20 {
		limit = 20
	}

	// BOLT: Pre-allocate slice to avoid multiple reallocations during append.
	cardNames := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		cardNames = append(cardNames, cards[i].Name)
	}

	prompt := fmt.Sprintf(`Based on the following cards in a binder, suggest a single, creative, and premium-sounding name for the binder: %s.
Respond ONLY with the name, no quotes or explanations.`, strings.Join(cardNames, ", "))

	response, err := s.queryLLM(prompt)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(response), nil
}
