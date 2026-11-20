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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"pokget/internal/models"
)

const (
	defaultOllamaHost    = "pokget_ollama"
	defaultOllamaModel   = "qwen2.5:1.5b"
	defaultLLMNumPredict = 128
	defaultLLMNumContext = 2048
	defaultLLMNumThread  = 4
	defaultLLMSeed       = 42
)

// LLMConfig configures a primary OpenAI-compatible provider and an Ollama fallback.
type LLMConfig struct {
	PrimaryBaseURL   string
	PrimaryModel     string
	PrimaryAPIKey    string
	PrimaryMaxTokens int
	BaseURL          string
	Model            string
	HTTPClient       *http.Client
	Timeout          time.Duration
	Temperature      float64
	Seed             int
	NumPredict       int
	NumContext       int
	NumThread        int
}

// LLMService sends text prompts (binder naming) to the configured LLM providers.
type LLMService struct {
	PrimaryBaseURL   string
	PrimaryModel     string
	PrimaryAPIKey    string
	PrimaryMaxTokens int
	Timeout          time.Duration
	BaseURL          string
	Model            string
	HTTPClient       *http.Client
	Temperature      float64
	Seed             int
	NumPredict       int
	NumContext       int
	NumThread        int
}

// NewLLMService enables the primary provider only when LLM_BASE_URL is configured.
func NewLLMService() *LLMService {
	config := LLMConfig{
		PrimaryBaseURL:   os.Getenv("LLM_BASE_URL"),
		PrimaryModel:     os.Getenv("LLM_MODEL"),
		PrimaryAPIKey:    os.Getenv("LLM_API"),
		PrimaryMaxTokens: envInt("LLM_MAX_TOKENS", defaultLLMPrimaryMaxTokens),
		BaseURL:          os.Getenv("OLLAMA_HOST"),
		Model:            envString("OLLAMA_MODEL", defaultOllamaModel),
		Temperature:      envFloat("OLLAMA_TEMPERATURE", 0),
		Seed:             envInt("OLLAMA_SEED", defaultLLMSeed),
		NumPredict:       envInt("OLLAMA_NUM_PREDICT", defaultLLMNumPredict),
		NumContext:       envInt("OLLAMA_NUM_CTX", defaultLLMNumContext),
		NumThread:        envInt("OLLAMA_NUM_THREAD", defaultLLMNumThread),
	}
	service, err := NewLLMServiceWithConfig(config)
	if err == nil {
		return service
	}
	slog.Warn("LLM: Invalid environment configuration; disabling primary provider", "error", err)
	config.PrimaryBaseURL, config.PrimaryModel, config.PrimaryAPIKey = "", "", ""
	service, err = NewLLMServiceWithConfig(config)
	if err == nil {
		return service
	}
	service, _ = NewLLMServiceWithConfig(LLMConfig{})
	return service
}

// NewLLMServiceWithConfig validates provider URLs and generation options.
func NewLLMServiceWithConfig(config LLMConfig) (*LLMService, error) {
	baseURL, err := normalizeOllamaBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	primaryURL, err := normalizePrimaryLLMBaseURL(config.PrimaryBaseURL)
	if err != nil {
		return nil, err
	}
	if primaryURL != "" {
		config.PrimaryModel = strings.TrimSpace(config.PrimaryModel)
		if config.PrimaryModel == "" {
			return nil, errors.New("llm: primary model is required")
		}
		config.PrimaryAPIKey = strings.TrimSpace(config.PrimaryAPIKey)
		if strings.ContainsAny(config.PrimaryAPIKey, "\r\n") {
			return nil, errors.New("llm: invalid primary API key")
		}
	} else {
		config.PrimaryModel, config.PrimaryAPIKey = "", ""
	}
	if config.PrimaryMaxTokens <= 0 {
		config.PrimaryMaxTokens = defaultLLMPrimaryMaxTokens
	}
	if config.Model == "" {
		config.Model = defaultOllamaModel
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Minute
		if primaryURL != "" {
			config.Timeout = defaultLLMRequestTimeout
		}
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.Timeout}
	}
	if config.Seed == 0 {
		config.Seed = defaultLLMSeed
	}
	if config.NumPredict <= 0 {
		config.NumPredict = defaultLLMNumPredict
	}
	if config.NumContext <= 0 {
		config.NumContext = defaultLLMNumContext
	}
	if config.NumThread <= 0 {
		config.NumThread = defaultLLMNumThread
	}
	if config.Temperature < 0 || config.Temperature > 2 {
		return nil, fmt.Errorf("llm: temperature %.2f outside [0,2]", config.Temperature)
	}

	return &LLMService{
		PrimaryBaseURL:   primaryURL,
		PrimaryModel:     config.PrimaryModel,
		PrimaryAPIKey:    config.PrimaryAPIKey,
		PrimaryMaxTokens: config.PrimaryMaxTokens,
		Timeout:          config.Timeout,
		BaseURL:          baseURL,
		Model:            config.Model,
		HTTPClient:       config.HTTPClient,
		Temperature:      config.Temperature,
		Seed:             config.Seed,
		NumPredict:       config.NumPredict,
		NumContext:       config.NumContext,
		NumThread:        config.NumThread,
	}, nil
}

func fallbackOllamaHost() string {
	host := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if host == "" || host == "0.0.0.0" {
		return defaultOllamaHost
	}
	return host
}

func normalizeOllamaBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	fallbackHost := fallbackOllamaHost()
	if raw == "" {
		raw = fallbackHost
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("llm: invalid Ollama host %q", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("llm: unsupported Ollama URL scheme %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", errors.New("llm: Ollama URL must not contain user information")
	}
	if parsed.Hostname() == "0.0.0.0" {
		port := parsed.Port()
		if port == "" {
			port = "11434"
		}
		parsed.Host = net.JoinHostPort(fallbackHost, port)
	} else if parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), "11434")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil {
		return fallback
	}
	return value
}

// AutoSetupContext ensures the configured Ollama model is available.
func (s *LLMService) AutoSetupContext(ctx context.Context) {
	if strings.TrimSpace(os.Getenv("OLLAMA_HOST")) == "" && (s.BaseURL == "" || s.BaseURL == "http://"+defaultOllamaHost+":11434") {
		return
	}
	slog.Info("LLM: Auto-setup started")
	baseURL, model, client := s.BaseURL, s.Model, s.httpClient()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		slog.Error("LLM: Failed to create model check request", "error", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("LLM: Failed to check models", "error", err)
		}
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
	for _, candidate := range tagsResp.Models {
		if strings.HasPrefix(candidate.Name, model) {
			slog.Info("LLM: Model already exists", "model", model)
			return
		}
	}

	slog.Info("LLM: Model not found, pulling...", "model", model)
	payload := map[string]any{"model": model, "stream": false}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		slog.Error("LLM: Failed to marshal model pull request", "error", err)
		return
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(jsonData))
	if err != nil {
		slog.Error("LLM: Failed to create model pull request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("LLM: Failed to pull model", "error", err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("LLM: Pull API returned error", "status", resp.StatusCode)
		return
	}
	slog.Info("LLM: Model pulled successfully", "model", model)
}

func (s *LLMService) queryOllamaRequest(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model":  s.Model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": s.Temperature,
			"seed":        s.effectiveSeed(),
			"num_predict": s.effectiveNumPredict(),
			"num_ctx":     s.effectiveNumContext(),
			"num_thread":  s.effectiveNumThread(),
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/api/generate", bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("llm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm API returned status %d", resp.StatusCode)
	}
	var result struct {
		Response   string `json:"response"`
		Done       *bool  `json:"done"`
		DoneReason string `json:"done_reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("llm: decode Ollama response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errors.New("llm: trailing Ollama response data")
	}
	if strings.TrimSpace(result.Response) == "" || result.DoneReason == "length" || (result.Done != nil && !*result.Done) {
		return "", errors.New("llm: Ollama returned an incomplete response")
	}
	return result.Response, nil
}

func (s *LLMService) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

func (s *LLMService) effectiveSeed() int {
	if s.Seed == 0 {
		return defaultLLMSeed
	}
	return s.Seed
}

func (s *LLMService) effectiveNumPredict() int {
	if s.NumPredict <= 0 {
		return defaultLLMNumPredict
	}
	return s.NumPredict
}

func (s *LLMService) effectiveNumContext() int {
	if s.NumContext <= 0 {
		return defaultLLMNumContext
	}
	return s.NumContext
}

func (s *LLMService) effectiveNumThread() int {
	if s.NumThread <= 0 {
		return defaultLLMNumThread
	}
	return s.NumThread
}

// GenerateBinderNameContext keeps a primary naming request attached to its
// caller, including while waiting between failed provider attempts.
func (s *LLMService) GenerateBinderNameContext(ctx context.Context, cards []models.Card) (string, error) {
	if len(cards) == 0 {
		return "New Empty Binder", nil
	}
	limit := min(len(cards), 20)
	cardNames := make([]string, 0, limit)
	for index := range limit {
		cardNames = append(cardNames, cards[index].Name)
	}
	prompt := fmt.Sprintf(`Based on the following cards in a binder, suggest a single, creative, and premium-sounding name for the binder: %s.
Respond ONLY with the name, no quotes or explanations.`, strings.Join(cardNames, ", "))
	response, err := s.queryLLMContext(ctx, prompt)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(response), nil
}
