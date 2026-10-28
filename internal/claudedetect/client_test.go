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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	_, err := New(Config{APIKey: ""})
	if err != ErrAPIKeyRequired {
		t.Fatalf("expected ErrAPIKeyRequired, got %v", err)
	}

	c, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Model() != DefaultModel {
		t.Errorf("expected default model %s, got %s", DefaultModel, c.Model())
	}
	if c.PromptCachingEnabled() {
		t.Errorf("expected PromptCaching false by default")
	}

	c2, err := New(Config{
		APIKey:        "test-key",
		Model:         "claude-opus-5-5",
		PromptCaching: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c2.Model() != "claude-opus-5-5" {
		t.Errorf("expected claude-opus-5-5, got %s", c2.Model())
	}
	if !c2.PromptCachingEnabled() {
		t.Errorf("expected PromptCaching true")
	}
}

func TestDetectSonnetResponse(t *testing.T) {
	var receivedBeta string
	var receivedKey string
	var receivedSysPromptCached bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBeta = r.Header.Get("anthropic-beta")
		receivedKey = r.Header.Get("x-api-key")

		var payload struct {
			System []struct {
				Type         string            `json:"type"`
				CacheControl map[string]string `json:"cache_control"`
			} `json:"system"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload.System) > 0 && payload.System[0].CacheControl["type"] == "ephemeral" {
			receivedSysPromptCached = true
		}

		resp := map[string]any{
			"model": "claude-sonnet-5-5",
			"content": []map[string]any{
				{
					"type": "text",
					"text": `{"game":"pokemon","name":"Rayquaza","collector_number":"16/110","set":"EX Holon Phantoms","language":"deu","variant":"Holo Delta Species","confidence":0.98}`,
				},
			},
			"usage": map[string]any{
				"input_tokens":                15,
				"output_tokens":               45,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     1774,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:        "test-key-123",
		Model:         "claude-sonnet-5-5",
		BaseURL:       server.URL,
		PromptCaching: true,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0x00, 0x11, 0x22}
	info, err := client.Detect(context.Background(), fakeJPEG, "pokemon", "deu")
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}

	if receivedKey != "test-key-123" {
		t.Errorf("expected x-api-key test-key-123, got %s", receivedKey)
	}
	if receivedBeta != PromptCachingBeta {
		t.Errorf("expected anthropic-beta %s, got %s", PromptCachingBeta, receivedBeta)
	}
	if !receivedSysPromptCached {
		t.Errorf("expected system prompt to have cache_control ephemeral")
	}

	if info.Name != "Rayquaza" {
		t.Errorf("expected name Rayquaza, got %s", info.Name)
	}
	if info.CollectorNumber != "16/110" {
		t.Errorf("expected collector_number 16/110, got %s", info.CollectorNumber)
	}
	if info.Language != "deu" {
		t.Errorf("expected language deu, got %s", info.Language)
	}
	if info.CacheReadTokens != 1774 {
		t.Errorf("expected CacheReadTokens 1774, got %d", info.CacheReadTokens)
	}
}

func TestDetectOpusThinkingResponse(t *testing.T) {
	// Opus 5.5 returns a thinking block before the text block
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"model": "claude-opus-5-5",
			"content": []map[string]any{
				{
					"type":     "thinking",
					"thinking": "Analyzing card features... Charizard Base Set 4/102...",
				},
				{
					"type": "text",
					"text": "```json\n{\n  \"game\": \"pokemon\",\n  \"name\": \"Charizard\",\n  \"collector_number\": \"4/102\",\n  \"set\": \"Base Set\",\n  \"language\": \"eng\",\n  \"variant\": \"Holo Rare\",\n  \"confidence\": 0.99\n}\n```",
				},
			},
			"usage": map[string]any{
				"input_tokens":  25,
				"output_tokens": 120,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:  "test-key",
		Model:   "claude-opus-5-5",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	fakePNG := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	info, err := client.Detect(context.Background(), fakePNG, "pokemon", "eng")
	if err != nil {
		t.Fatalf("detect failed: %v", err)
	}

	if info.Name != "Charizard" {
		t.Errorf("expected Charizard, got %s", info.Name)
	}
	if info.CollectorNumber != "4/102" {
		t.Errorf("expected 4/102, got %s", info.CollectorNumber)
	}
	if info.Set != "Base Set" {
		t.Errorf("expected Base Set, got %s", info.Set)
	}
}

func TestDetectValidation(t *testing.T) {
	client, _ := New(Config{APIKey: "key"})

	_, err := client.Detect(context.Background(), nil, "", "")
	if err != ErrEmptyImage {
		t.Errorf("expected ErrEmptyImage, got %v", err)
	}

	huge := make([]byte, maxImageBytes+1)
	_, err = client.Detect(context.Background(), huge, "", "")
	if err != ErrImageTooLarge {
		t.Errorf("expected ErrImageTooLarge, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Detect(ctx, []byte{1, 2, 3}, "", "")
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDetectImageMimeType(t *testing.T) {
	if detectImageMimeType([]byte{0xFF, 0xD8, 0xFF, 0xE0}) != "image/jpeg" {
		t.Errorf("JPEG detection failed")
	}
	if detectImageMimeType([]byte("\x89PNG\r\n\x1a\n")) != "image/png" {
		t.Errorf("PNG detection failed")
	}
	if detectImageMimeType([]byte("RIFF1234WEBP")) != "image/webp" {
		t.Errorf("WebP detection failed")
	}
	if detectImageMimeType([]byte("GIF89a")) != "image/gif" {
		t.Errorf("GIF detection failed")
	}
	if detectImageMimeType([]byte("random")) != "image/jpeg" {
		t.Errorf("Fallback to image/jpeg failed")
	}
}

func TestExtractJSONString(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{`{"a": 1}`, `{"a": 1}`},
		{"```json\n{\"a\": 1}\n```", `{"a": 1}`},
		{"```\n{\"a\": 1}\n```", `{"a": 1}`},
		{"Here is the JSON:\n{\"a\": 1}\nHope this helps!", `{"a": 1}`},
	}
	for _, c := range cases {
		out := extractJSONString(c.input)
		if out != c.expected {
			t.Errorf("expected %q, got %q", c.expected, out)
		}
	}
}
