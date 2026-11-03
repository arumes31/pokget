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
	"strings"
	"time"
)

const (
	defaultLLMPrimaryMaxTokens = 1024
	defaultLLMRequestTimeout   = 50 * time.Second
	primaryLLMAttempts         = 3
	primaryLLMRetryPause       = 5 * time.Second
)

func normalizePrimaryLLMBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("llm: invalid primary base URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("llm: primary URL must not contain credentials, query, or fragment")
	}
	loopback := parsed.Hostname() == "localhost"
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return "", errors.New("llm: primary URL must use HTTPS except on loopback")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// queryLLMWithFallback validates each provider before accepting its response.
// An explicit abstention is a successful result, never a reason to ask another model.
func (s *LLMService) queryLLMWithFallback(ctx context.Context, prompt string, format any, validate func(string, bool) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.PrimaryBaseURL != "" {
		for attempt := 1; attempt <= primaryLLMAttempts; attempt++ {
			// A slow completion is still a live attempt. Only the caller's
			// cancellation or an actual provider failure ends it.
			response, err := s.queryPrimaryLLM(ctx, prompt, format)
			if err == nil && validate != nil {
				err = validate(response, true)
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err == nil {
				slog.Info("LLM: primary request succeeded", "provider", "openai-compatible", "model", s.PrimaryModel, "attempt", attempt)
				return response, nil
			}
			// Provider errors can contain private response text or URLs.
			slog.Warn("LLM: primary attempt did not produce a usable response", "attempt", attempt, "max_attempts", primaryLLMAttempts)
			if attempt < primaryLLMAttempts {
				timer := time.NewTimer(primaryLLMRetryPause)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", ctx.Err()
				case <-timer.C:
				}
			}
		}
		slog.Warn("LLM: primary attempts exhausted; trying Ollama")
	}
	// The text fallback keeps its own bounded budget, starting after primary.
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultLLMRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := s.queryOllamaRequest(ctx, prompt, format)
	if err == nil && validate != nil {
		err = validate(response, false)
	}
	if s.PrimaryBaseURL != "" && err == nil {
		slog.Info("LLM: fallback request succeeded", "provider", "ollama", "model", s.Model)
	}
	return response, err
}

func (s *LLMService) queryPrimaryLLM(ctx context.Context, prompt string, format any) (string, error) {
	maxTokens := s.PrimaryMaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultLLMPrimaryMaxTokens
	}
	payload := map[string]any{
		"model":       s.PrimaryModel,
		"messages":    []any{map[string]any{"role": "user", "content": prompt}},
		"temperature": s.Temperature,
		"max_tokens":  maxTokens,
		"stream":      false,
	}
	if format != nil {
		payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "card_selection", "strict": true, "schema": format}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("llm: encode primary request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.PrimaryBaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", errors.New("llm: cannot create primary request")
	}
	req.Header.Set("Content-Type", "application/json")
	if s.PrimaryAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.PrimaryAPIKey)
	}
	client := *s.httpClient()
	client.Timeout = 0
	// Never forward the bearer token or private card image through a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: primary request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm: primary returned status %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&result); err != nil {
		return "", errors.New("llm: malformed primary response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errors.New("llm: trailing primary response data")
	}
	if len(result.Choices) != 1 || strings.TrimSpace(result.Choices[0].Message.Content) == "" || result.Choices[0].FinishReason == "length" {
		return "", errors.New("llm: primary response has no complete answer")
	}
	return result.Choices[0].Message.Content, nil
}
