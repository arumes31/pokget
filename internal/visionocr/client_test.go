package visionocr_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pokget/internal/visionocr"
)

func imageBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jC1sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func completedResponse(text string) string {
	body, _ := json.Marshal(map[string]any{"done": true, "done_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}})
	return string(body)
}

func newClient(t *testing.T, config visionocr.Config) *visionocr.Client {
	t.Helper()
	client, err := visionocr.New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	for _, address := range []string{"", "localhost:11434", "/api", "ftp://localhost", "http://", "http://user:secret@localhost", "http://localhost?token=secret", "http://localhost#fragment"} {
		t.Run(address, func(t *testing.T) {
			if _, err := visionocr.New(visionocr.Config{BaseURL: address}); err == nil {
				t.Fatal("expected invalid base URL to be rejected")
			}
		})
	}
}

func TestExtractUsesNativeImageOCRRequest(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		threads     int
		wantModel   string
		wantThreads int
	}{
		{name: "defaults", wantModel: "qwen3.5:0.8b", wantThreads: 4},
		{name: "configured", model: "qwen3.5:custom", threads: 8, wantModel: "qwen3.5:custom", wantThreads: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := imageBytes(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
					t.Errorf("request = %s %s, want POST /api/chat", r.Method, r.URL.Path)
				}
				var request struct {
					Model    string `json:"model"`
					Stream   *bool  `json:"stream"`
					Messages []struct {
						Content string
						Images  []string
					} `json:"messages"`
					Options map[string]any `json:"options"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				if request.Model != tc.wantModel || len(request.Messages) != 1 {
					t.Errorf("model/messages = %q/%+v", request.Model, request.Messages)
					return
				}
				message := request.Messages[0]
				if message.Content != "Read all visible text in this image. Preserve the original language and numbers. Output only the text, with no commentary or markdown fences. Do not guess unreadable text." {
					t.Errorf("unexpected transcription prompt: %q", message.Content)
				}
				if request.Stream == nil || *request.Stream {
					t.Error("stream must be explicitly false")
				}
				if len(message.Images) != 1 || message.Images[0] != base64.StdEncoding.EncodeToString(input) {
					t.Errorf("images = %v, want one original image in base64", message.Images)
				}
				if request.Options["num_gpu"] != float64(0) || request.Options["num_thread"] != float64(tc.wantThreads) {
					t.Errorf("CPU options = %v", request.Options)
				}
				if request.Options["num_predict"] != float64(512) || request.Options["num_ctx"] != float64(4096) {
					t.Errorf("generation budget changed: %v", request.Options)
				}
				_, _ = io.WriteString(w, completedResponse("Furret\n061/109"))
			}))
			defer server.Close()
			client := newClient(t, visionocr.Config{BaseURL: server.URL, Model: tc.model, Threads: tc.threads})
			if text, err := client.Extract(context.Background(), input); err != nil || text != "Furret\n061/109" {
				t.Fatalf("Extract = %q, %v", text, err)
			}
		})
	}
}

func TestExtractPreservesMultilingualEvidence(t *testing.T) {
	for _, tc := range []struct{ language, text string }{
		{"en", "Charizard ex\nHP 330\n199/165"},
		{"de", "Glurak ex\nKP 330\n199/165"},
		{"fr", "Dracaufeu ex\nÉnergie défaussée\n199/165"},
		{"ja", "リザードンex\n悪テラスタル\n199/165"},
		{"zh-hans", "喷火龙ex\n宝可梦\n199/165"},
		{"zh-hant", "噴火龍ex\n寶可夢\n199/165"},
		{"ko", "리자몽 ex\n포켓몬\n199/165"},
		{"short repeated symbols are legitimate", "HP 100\n0\n0\n0\n火\n火\n火\n061/109"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, completedResponse(tc.text)) }))
			defer server.Close()
			client := newClient(t, visionocr.Config{BaseURL: server.URL})
			text, err := client.Extract(context.Background(), imageBytes(t))
			if err != nil || text != tc.text {
				t.Fatalf("Extract = %q, %v; want exact Unicode evidence %q", text, err, tc.text)
			}
		})
	}
}

func TestExtractRejectsIncompleteAndMalformedEvidence(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unfinished Furret", `{"done":false,"done_reason":"stop","message":{"role":"assistant","content":"Furret\n136/189"}}`},
		{"Luffy length termination", `{"done":true,"done_reason":"length","message":{"role":"assistant","content":"Monkey.D.Luffy\nST01-001"}}`},
		{"missing completion flag", `{"done_reason":"stop","message":{"role":"assistant","content":"Aggron ex"}}`},
		{"missing completion reason", `{"done":true,"message":{"role":"assistant","content":"Aggron ex"}}`},
		{"unknown completion reason", `{"done":true,"done_reason":"cancelled","message":{"role":"assistant","content":"Aggron ex"}}`},
		{"empty", completedResponse("")},
		{"whitespace", completedResponse(" \n\t")},
		{"missing text", `{"done":true,"done_reason":"stop"}`},
		{"missing assistant content", `{"done":true,"done_reason":"stop","message":{"role":"assistant"}}`},
		{"null assistant content", `{"done":true,"done_reason":"stop","message":{"role":"assistant","content":null}}`},
		{"nontext", `{"done":true,"done_reason":"stop","message":{"role":"assistant","content":["Aggron ex"]}}`},
		{"truncated JSON", `{"done":true,"message":{"role":"assistant","content":"Aggron`},
		{"trailing JSON", completedResponse("Furret") + `{}`},
		{"trailing garbage", completedResponse("Furret") + "garbage"},
		{"oversized envelope", completedResponse("Furret") + strings.Repeat(" ", 1<<20)},
		{"oversized text", completedResponse(strings.Repeat("a", (32<<10)+1))},
		{"repeated substantive lines", completedResponse("Monkey.D.Luffy\nMonkey.D.Luffy\nMonkey.D.Luffy\nOP01-003")},
		{"short Latin repetition", completedResponse("Furret\nFurret\nFurret\n136/189")},
		{"short Chinese repetition", completedResponse("喷火龙ex\n喷火龙ex\n喷火龙ex\n199/165")},
		{"two glyph name repetition", completedResponse("超夢\n超夢\n超夢\n199/165")},
		{"short Korean repetition", completedResponse("파이리\n파이리\n파이리\n199/165")},
		{"repeated separated lines", completedResponse("Monkey.D.Luffy\n6000\nMonkey.D.Luffy\n5\nMonkey.D.Luffy")},
		{"pilot repeated fence run", completedResponse("Monkey.D.Luffy\nOP01-003\n```\n```\n```")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			client := newClient(t, visionocr.Config{BaseURL: server.URL})
			text, err := client.Extract(context.Background(), imageBytes(t))
			if !errors.Is(err, visionocr.ErrInvalidResponse) || text != "" {
				t.Fatalf("Extract = %q, %v; want no evidence and ErrInvalidResponse", text, err)
			}
		})
	}
}

func TestExtractRejectsOversizedInputBeforeSending(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, completedResponse("Furret"))
	}))
	defer server.Close()
	client := newClient(t, visionocr.Config{BaseURL: server.URL})
	if _, err := client.Extract(context.Background(), make([]byte, (8<<20)+1)); err == nil {
		t.Fatal("oversized image accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("oversized image reached the server")
	}
}

func TestExtractDoesNotLeakHTTPErrorBody(t *testing.T) {
	const privateText = "PRIVATE_CARD_AND_PROVIDER_DETAILS"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, privateText, http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newClient(t, visionocr.Config{BaseURL: server.URL})
	text, err := client.Extract(context.Background(), imageBytes(t))
	if err == nil || text != "" {
		t.Fatalf("Extract = %q, %v; want HTTP error without evidence", text, err)
	}
	if strings.Contains(err.Error(), privateText) {
		t.Fatalf("HTTP error leaked response body: %v", err)
	}
}

func TestExtractDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = io.WriteString(w, completedResponse("Furret"))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := newClient(t, visionocr.Config{BaseURL: server.URL})
	if _, err := client.Extract(context.Background(), imageBytes(t)); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect target received a request")
	}
}

func TestExtractSingleFlightAndCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		_, _ = io.WriteString(w, completedResponse("Furret"))
	}))
	defer server.Close()
	defer close(release)
	client := newClient(t, visionocr.Config{BaseURL: server.URL})
	input := imageBytes(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := client.Extract(ctx, input); first <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached server")
	}
	busyCtx, busyCancel := context.WithTimeout(context.Background(), time.Second)
	defer busyCancel()
	if _, err := client.Extract(busyCtx, input); !errors.Is(err, visionocr.ErrBusy) {
		t.Fatalf("second Extract error = %v, want ErrBusy", err)
	}
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Extract error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop request")
	}
	if text, err := client.Extract(context.Background(), input); err != nil || text != "Furret" {
		t.Fatalf("slot not reusable after cancellation: %q, %v", text, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("server received %d requests; want 2", calls.Load())
	}
}

func TestExtractHonorsConfiguredTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client := newClient(t, visionocr.Config{BaseURL: server.URL, Timeout: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.Extract(ctx, imageBytes(t))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want DeadlineExceeded", err)
	}
	if ctx.Err() != nil {
		t.Fatal("parent timeout fired instead of configured client timeout")
	}
}

func TestExtractCancelsDuringResponseBody(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"done":true,"message":{"role":"assistant","content":"`)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	client := newClient(t, visionocr.Config{BaseURL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := client.Extract(ctx, imageBytes(t)); finished <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("body never started")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("body read ignored cancellation")
	}
}
