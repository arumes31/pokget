package visionocr_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/visionocr"
)

func TestExtractUsesNonThinkingVisionChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("endpoint = %q, want /api/chat", r.URL.Path)
		}
		var body struct {
			Think    *bool `json:"think"`
			Messages []struct {
				Role, Content string
				Images        []string
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Think == nil || *body.Think {
			t.Error("thinking must be explicitly disabled")
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content == "" || len(body.Messages[0].Images) != 1 {
			t.Errorf("image chat message missing: %+v", body.Messages)
		}
		_, _ = io.WriteString(w, `{"done":true,"done_reason":"stop","message":{"role":"assistant","content":"Furret\n136/189"}}`)
	}))
	defer server.Close()
	client := newClient(t, visionocr.Config{BaseURL: server.URL, Model: "replacement-test"})
	text, err := client.Extract(context.Background(), imageBytes(t))
	if err != nil || text != "Furret\n136/189" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestExtractRejectsNonTranscriptionChatResponses(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"thinking without content", `{"done":true,"done_reason":"stop","message":{"role":"assistant","thinking":"Furret 136/189"}}`},
		{"tool call", `{"done":true,"done_reason":"stop","message":{"role":"assistant","content":"Furret 136/189","tool_calls":[{"function":{"name":"lookup"}}]}}`},
		{"wrong role", `{"done":true,"done_reason":"stop","message":{"role":"user","content":"Furret 136/189"}}`},
		{"provider error", `{"error":"private provider details","done":true,"done_reason":"stop","message":{"role":"assistant","content":"Furret 136/189"}}`},
		{"legacy generate envelope", `{"done":true,"done_reason":"stop","response":"Furret 136/189"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			client := newClient(t, visionocr.Config{BaseURL: server.URL})
			text, err := client.Extract(context.Background(), imageBytes(t))
			if text != "" || !errors.Is(err, visionocr.ErrInvalidResponse) {
				t.Fatalf("text=%q err=%v", text, err)
			}
			if strings.Contains(err.Error(), "private provider details") {
				t.Fatal("provider error leaked private details")
			}
		})
	}
}
