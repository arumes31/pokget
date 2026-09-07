package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pokget/internal/service"
)

type binderCancellationTransport func(*http.Request) (*http.Response, error)

func (transport binderCancellationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestAutoNameBinderCancelsPrimaryWithCaller(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	request := authedRequest(t, http.MethodPost, "/binders/auto-name", "binder_id=b1")
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	expectBinderOwned(mock, "b1", "test-user", "Pikachu")
	propagated := false
	llm, err := service.NewLLMServiceWithConfig(service.LLMConfig{
		PrimaryBaseURL: "https://primary.example/v1",
		PrimaryModel:   "test-model",
		HTTPClient: &http.Client{Transport: binderCancellationTransport(func(outbound *http.Request) (*http.Response, error) {
			cancel()
			select {
			case <-outbound.Context().Done():
				propagated = true
				return nil, outbound.Context().Err()
			case <-time.After(50 * time.Millisecond):
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"Example binder"},"finish_reason":"stop"}]}`)),
					Header:     make(http.Header),
				}, nil
			}
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.LLM = llm
	recorder := httptest.NewRecorder()
	h.AutoNameBinder(recorder, request.WithContext(ctx))
	if !propagated {
		t.Fatal("closing the binder-name request did not cancel the primary request")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
