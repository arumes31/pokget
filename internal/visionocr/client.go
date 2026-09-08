// Package visionocr provides bounded, CPU-only image transcription via Ollama.
// Transcriptions are untrusted evidence, not card identities or language labels.
package visionocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidResponse = errors.New("vision OCR: incomplete or invalid response")
	ErrBusy            = errors.New("vision OCR: already processing an image")
)

const (
	maxImageBytes       = 8 << 20
	maxResponseBytes    = 1 << 20
	maxTextBytes        = 32 << 10
	transcriptionPrompt = "Read all visible text in this image. Preserve the original language and numbers. Output only the text, with no commentary or markdown fences. Do not guess unreadable text."
)

type Config struct {
	BaseURL string
	Model   string
	Timeout time.Duration
	Threads int
}

// Client permits one inference at a time, without queuing interactive scans.
// Configuration is immutable; Extract is safe for concurrent callers.
type Client struct {
	endpoint string
	model    string
	threads  int
	timeout  time.Duration
	http     *http.Client
	busy     atomic.Bool
}

func New(config Config) (*Client, error) {
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("vision OCR: base URL must be absolute HTTP(S), without credentials, query, or fragment")
	}
	if config.Timeout < 0 || config.Threads < 0 {
		return nil, errors.New("vision OCR: timeout and threads must be positive")
	}
	if config.Model == "" {
		config.Model = "qwen3.5:0.8b"
	}
	if config.Timeout == 0 {
		config.Timeout = 45 * time.Second
	}
	if config.Threads == 0 {
		config.Threads = 4
	}
	return &Client{
		endpoint: strings.TrimRight(base.String(), "/") + "/api/chat",
		model:    config.Model, threads: config.Threads, timeout: config.Timeout,
		http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) Extract(ctx context.Context, image []byte) (string, error) {
	if len(image) == 0 || len(image) > maxImageBytes {
		return "", errors.New("vision OCR: image must be between 1 byte and 8 MiB")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !c.busy.CompareAndSwap(false, true) {
		return "", ErrBusy
	}
	defer c.busy.Store(false)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// []byte is encoded as base64 by encoding/json. Ask for transcription,
	// not card identities, and disable thinking to bound CPU generation.
	body, err := json.Marshal(map[string]any{
		"model": c.model, "stream": false, "think": false,
		"messages":   []map[string]any{{"role": "user", "content": transcriptionPrompt, "images": [][]byte{image}}},
		"keep_alive": "5m",
		// Qwen's recommended non-thinking vision sampling, with a bounded
		// output length for card scans. Keep parity with the live benchmark.
		"options": map[string]any{"num_gpu": 0, "num_thread": c.threads, "num_ctx": 4096, "num_predict": 512, "temperature": 0.7, "top_p": 0.8, "top_k": 20, "min_p": 0, "presence_penalty": 1.5, "repeat_penalty": 1, "seed": 42},
	})
	if err != nil {
		return "", fmt.Errorf("vision OCR: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("vision OCR: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Do not expose endpoint credentials, network details, or scanned text.
		return "", errors.New("vision OCR: provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision OCR: provider HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil || len(data) > maxResponseBytes || !utf8.Valid(data) {
		return "", ErrInvalidResponse
	}
	var result struct {
		Done    bool   `json:"done"`
		Reason  string `json:"done_reason"`
		Error   string `json:"error"`
		Message struct {
			Role      string            `json:"role"`
			Content   string            `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	}
	// Unmarshal rejects trailing JSON/garbage as well as truncated envelopes.
	if err := json.Unmarshal(data, &result); err != nil || !result.Done || result.Reason != "stop" || result.Error != "" || result.Message.Role != "assistant" || len(result.Message.ToolCalls) != 0 || !validText(result.Message.Content) {
		return "", ErrInvalidResponse
	}
	return result.Message.Content, nil
}

func validText(text string) bool {
	if strings.TrimSpace(text) == "" || len(text) > maxTextBytes || !utf8.ValidString(text) {
		return false
	}
	lines := make(map[string]int)
	fences := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fences++
			if fences >= 3 {
				return false
			}
		} else {
			fences = 0
		}
		// Card costs and single energy glyphs legitimately repeat. Short
		// CJK names are still substantive: count letters, not byte length.
		letters := 0
		for _, r := range line {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters < 2 {
			continue
		}
		lines[line]++
		if lines[line] >= 3 {
			return false
		}
	}
	return true
}
