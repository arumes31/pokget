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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLLMAutoSetupContextCancelsRequest(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	service := &LLMService{
		BaseURL:    server.URL,
		Model:      "test-model",
		HTTPClient: server.Client(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	service.AutoSetupContext(ctx)
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("canceled model setup returned after %s", elapsed)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("model setup request never reached test server")
	}
}

func TestNewLLMServiceWithConfigNormalizesHostAndOptions(t *testing.T) {
	t.Parallel()

	service, err := NewLLMServiceWithConfig(LLMConfig{
		BaseURL: "ollama.internal", Model: "qwen-test", Temperature: 0.2,
		Seed: 99, NumPredict: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.BaseURL != "http://ollama.internal:11434" || service.Model != "qwen-test" {
		t.Fatalf("service endpoint/model = %q/%q", service.BaseURL, service.Model)
	}
	if service.Seed != 99 || service.NumPredict != 64 {
		t.Fatalf("service options = %+v", service)
	}

	zeroService, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: "0.0.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if zeroService.BaseURL != "http://pokget_ollama:11434" {
		t.Fatalf("expected 0.0.0.0 to normalize to pokget_ollama:11434, got %q", zeroService.BaseURL)
	}
}

func TestNormalizeOllamaBaseURLCustomHost(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "custom_ollama")
	customZeroService, err := NewLLMServiceWithConfig(LLMConfig{BaseURL: "0.0.0.0:8000"})
	if err != nil {
		t.Fatal(err)
	}
	if customZeroService.BaseURL != "http://custom_ollama:8000" {
		t.Fatalf("expected custom OLLAMA_HOST normalization, got %q", customZeroService.BaseURL)
	}
}
