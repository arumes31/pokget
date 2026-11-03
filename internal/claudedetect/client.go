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

package claudedetect

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultModel      = "claude-sonnet-5-5"
	DefaultOpusModel  = "claude-opus-5-5"
	DefaultHaikuModel = "claude-haiku-5-5"
	DefaultBaseURL    = "https://api.anthropic.com/v1/messages"
	DefaultTimeout    = 30 * time.Second
	AnthropicVersion  = "2023-06-01"
	PromptCachingBeta = "prompt-caching-2024-07-31"
	maxImageBytes     = 12 << 20 // 12 MiB
	maxResponseBytes  = 2 << 20  // 2 MiB
	// MaxImages bounds optional magnified crops sent alongside the full card.
	MaxImages = 3
)

var (
	ErrEmptyImage      = errors.New("claude detect: image bytes are empty")
	ErrImageTooLarge   = errors.New("claude detect: image exceeds maximum permitted size")
	ErrTooManyImages   = errors.New("claude detect: too many images for one request")
	ErrInvalidResponse = errors.New("claude detect: invalid or unparseable response from Claude API")
	ErrAPIKeyRequired  = errors.New("claude detect: API key is required")
)

type Config struct {
	APIKey        string
	Model         string
	BaseURL       string
	Timeout       time.Duration
	PromptCaching bool
	HTTPClient    *http.Client
	// Temperature is clamped to 0..1. Defaults to 0 so the same photo always
	// produces the same reading; sampling made scans of one image disagree.
	Temperature float64
}

// DetectedCardInfo holds the structured card metadata extracted by Claude.
type DetectedCardInfo struct {
	Game            string  `json:"game"`
	Name            string  `json:"name"`
	CollectorNumber string  `json:"collector_number"`
	Set             string  `json:"set"`
	SetCode         string  `json:"set_code"`
	Language        string  `json:"language"`
	Variant         string  `json:"variant"`
	Confidence      float64 `json:"confidence"`
	Notes           string  `json:"notes"`

	// Performance and cache metrics
	CacheCreationTokens int           `json:"-"`
	CacheReadTokens     int           `json:"-"`
	InputTokens         int           `json:"-"`
	OutputTokens        int           `json:"-"`
	ModelUsed           string        `json:"-"`
	Duration            time.Duration `json:"-"`
	RawText             string        `json:"-"`
}

type Client struct {
	apiKey        string
	model         string
	endpoint      string
	timeout       time.Duration
	promptCaching bool
	temperature   float64
	http          *http.Client
}

// New creates a new Claude detector client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrAPIKeyRequired
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	endpoint := cfg.BaseURL
	if endpoint == "" {
		endpoint = DefaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: timeout + 5*time.Second,
		}
	}

	return &Client{
		apiKey:        strings.TrimSpace(cfg.APIKey),
		model:         model,
		endpoint:      endpoint,
		timeout:       timeout,
		promptCaching: cfg.PromptCaching,
		temperature:   min(max(cfg.Temperature, 0), 1),
		http:          client,
	}, nil
}

// Model returns the configured model identifier.
func (c *Client) Model() string {
	return c.model
}

// PromptCachingEnabled returns whether prompt caching is enabled.
func (c *Client) PromptCachingEnabled() bool {
	return c.promptCaching
}

// Detect analyzes a single image of a card and returns its structured identity.
func (c *Client) Detect(ctx context.Context, imageBytes []byte, gameHint, langHint string) (*DetectedCardInfo, error) {
	return c.DetectImages(ctx, [][]byte{imageBytes}, gameHint, langHint)
}

// DetectImages analyzes the full card plus optional magnified crops. images[0]
// must be the complete card; images[1] is expected to be the title/name band
// and images[2] the bottom information line. The crops let the model read the
// small printed glyphs instead of guessing them from a downscaled card photo.
func (c *Client) DetectImages(ctx context.Context, images [][]byte, gameHint, langHint string) (*DetectedCardInfo, error) {
	if len(images) == 0 {
		return nil, ErrEmptyImage
	}
	if len(images) > MaxImages {
		return nil, ErrTooManyImages
	}
	for _, image := range images {
		if len(image) == 0 {
			return nil, ErrEmptyImage
		}
		if len(image) > maxImageBytes {
			return nil, ErrImageTooLarge
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	started := time.Now()

	content := make([]any, 0, len(images)*2+1)
	for index, image := range images {
		content = append(content,
			map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": detectImageMimeType(image),
					"data":       base64.StdEncoding.EncodeToString(image),
				},
			},
			map[string]any{"type": "text", "text": imageLabel(index, len(images))},
		)
	}
	content = append(content, map[string]any{"type": "text", "text": userPrompt(gameHint, langHint)})

	// Prepare request payload
	reqMap := map[string]any{
		"model":       c.model,
		"max_tokens":  2048,
		"temperature": c.temperature,
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
	}

	// Add cached system prompt
	if c.promptCaching {
		reqMap["system"] = []map[string]any{
			{
				"type": "text",
				"text": SystemPrompt,
				"cache_control": map[string]string{
					"type": "ephemeral",
				},
			},
		}
	} else {
		reqMap["system"] = []map[string]any{
			{
				"type": "text",
				"text": SystemPrompt,
			},
		}
	}

	reqBody, err := json.Marshal(reqMap)
	if err != nil {
		return nil, fmt.Errorf("claude detect: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("claude detect: create request: %w", err)
	}

	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)
	httpReq.Header.Set("content-type", "application/json")
	if c.promptCaching {
		httpReq.Header.Set("anthropic-beta", PromptCachingBeta)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		if callCtx.Err() != nil {
			return nil, callCtx.Err()
		}
		return nil, fmt.Errorf("claude detect: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("claude detect: API status %d: %s", resp.StatusCode, string(errBody))
	}

	respData, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("claude detect: read response: %w", err)
	}
	if len(respData) > maxResponseBytes {
		return nil, errors.New("claude detect: response body too large")
	}

	var apiResp struct {
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return nil, fmt.Errorf("claude detect: unmarshal response: %w", err)
	}

	// Extract the text block (skipping any thinking blocks)
	var rawText string
	for _, block := range apiResp.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			rawText = block.Text
			break
		}
	}

	if strings.TrimSpace(rawText) == "" {
		if apiResp.StopReason == "max_tokens" {
			return nil, fmt.Errorf("claude detect: response truncated by max_tokens limit (stop_reason=max_tokens)")
		}
		return nil, ErrInvalidResponse
	}

	// Extract JSON substring from rawText
	cleanJSON := extractJSONString(rawText)
	var info DetectedCardInfo
	if err := json.Unmarshal([]byte(cleanJSON), &info); err != nil {
		return nil, fmt.Errorf("claude detect: parse card JSON: %w (raw: %s)", err, rawText)
	}

	if info.Confidence <= 0 {
		info.Confidence = 0.50
	}
	if info.Confidence > 1.0 {
		info.Confidence = 1.0
	}

	info.Duration = time.Since(started)
	info.ModelUsed = apiResp.Model
	info.RawText = rawText
	info.InputTokens = apiResp.Usage.InputTokens
	info.OutputTokens = apiResp.Usage.OutputTokens
	info.CacheCreationTokens = apiResp.Usage.CacheCreationInputTokens
	info.CacheReadTokens = apiResp.Usage.CacheReadInputTokens

	return &info, nil
}

// imageLabel tells the model what each attached image is, so it compares the
// full card against the magnified crops instead of treating them as new cards.
func imageLabel(index, total int) string {
	switch {
	case total == 1:
		return "Image 1: the complete trading card."
	case index == 0:
		return "Image 1: the complete trading card. Use it for layout, artwork, symbols and foil."
	case index == 1:
		return "Image 2: magnified crop of the card's title area. Read the printed name here glyph by glyph."
	default:
		return "Image 3: magnified crop of the card's bottom information line. Read the collector number, set code, rarity and copyright here."
	}
}

// userPrompt states the collector's scope as a hard constraint and asks for an
// exact transcription. Sampling is disabled, so repeated scans of one photo
// return the same reading instead of drifting between similar glyphs.
func userPrompt(gameHint, langHint string) string {
	var prompt strings.Builder
	prompt.WriteString("Identify the trading card in these images. Return only the JSON object from the output contract.")
	prompt.WriteString("\n\nCollector scope (authoritative, do not override):")
	if gameHint != "" {
		prompt.WriteString("\n- game: " + gameHint + " (the JSON \"game\" value must be exactly this)")
	} else {
		prompt.WriteString("\n- game: infer from the card frame and symbols")
	}
	if langHint != "" {
		prompt.WriteString("\n- printed language: " + langHint + " (the JSON \"language\" value must match this language)")
	}
	prompt.WriteString("\n\nReading rules:")
	prompt.WriteString("\n1. Transcribe the name exactly as printed, in the printed script. Never translate, romanize or localize it.")
	prompt.WriteString("\n2. For Japanese kana, one small mark changes the card: ヒ/ビ/ピ, ハ/バ/パ, フ/ブ/プ, ヘ/ベ/ペ, ホ/ボ/ポ, カ/ガ, キ/ギ, ク/グ, ケ/ゲ, コ/ゴ, サ/ザ, シ/ジ, ス/ズ, セ/ゼ, ソ/ゾ, タ/ダ, チ/ヂ, ツ/ヅ, テ/デ, ト/ド. Re-read the marks in the magnified title crop before answering.")
	prompt.WriteString("\n3. Read the collector number and set code from the bottom crop. Never invent them; leave them empty when they are unreadable.")
	prompt.WriteString("\n4. If the title crop is unreadable, lower \"confidence\" below 0.7 instead of guessing a similar-looking name.")
	return prompt.String()
}

func detectImageMimeType(data []byte) string {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif"
	}
	return "image/jpeg"
}

func extractJSONString(s string) string {
	s = strings.TrimSpace(s)
	// If markdown wrapped with ```json ... ``` or ``` ... ```
	if idx := strings.Index(s, "```json"); idx != -1 {
		start := idx + 7
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	} else if idx := strings.Index(s, "```"); idx != -1 {
		start := idx + 3
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Look for first '{' and last '}'
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}
