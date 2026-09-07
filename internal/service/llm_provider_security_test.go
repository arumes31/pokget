package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"pokget/internal/models"
)

type primarySecurityTransport func(*http.Request) (*http.Response, error)

func (transport primarySecurityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func primarySecurityService(t *testing.T, primaryURL, fallbackURL string, client *http.Client) *LLMService {
	t.Helper()
	service, err := NewLLMServiceWithConfig(LLMConfig{
		PrimaryBaseURL: primaryURL,
		PrimaryModel:   "primary-model",
		PrimaryAPIKey:  "primary-secret-marker",
		BaseURL:        fallbackURL,
		Model:          "fallback-model",
		HTTPClient:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestLLMPrimaryRedirectNeverForwardsCredentialsOrImage(t *testing.T) {
	var redirectedCalls, fallbackCalls atomic.Int32
	redirected := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer redirected.Close()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer primary-secret-marker" || !strings.Contains(string(body), "data:image/png;base64,") {
			t.Error("initial primary request must carry the configured key and image")
		}
		http.Redirect(w, r, redirected.URL, http.StatusTemporaryRedirect)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "" || strings.Contains(string(body), "data:image/") || strings.Contains(string(body), "primary-secret-marker") {
			t.Error("primary credentials or image were forwarded to fallback")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"pikachu"}`})
	}))
	defer fallback.Close()
	service := primarySecurityService(t, primary.URL, fallback.URL, nil)
	imageData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	result, err := service.FuzzyMatchCardWithImageContext(context.Background(), "Pikachu", imageData, []models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if err != nil || result == nil || result.CardID != "pikachu" || redirectedCalls.Load() != 0 || fallbackCalls.Load() != 1 {
		t.Fatalf("result=%+v error=%v redirected=%d fallback=%d", result, err, redirectedCalls.Load(), fallbackCalls.Load())
	}
}

func TestLLMPrimaryNetworkFailureUsesFallback(t *testing.T) {
	var fallbackCalls atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("primary credential leaked to fallback")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": `{"card_id":"pikachu"}`})
	}))
	defer fallback.Close()
	client := &http.Client{Transport: primarySecurityTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "primary.invalid" {
			return nil, errors.New("network failure with private diagnostic marker")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	service := primarySecurityService(t, "https://primary.invalid/v1", fallback.URL, client)
	result, err := service.FuzzyMatchCardWithValidationContext(context.Background(), "Pikachu", []models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if err != nil || result == nil || result.CardID != "pikachu" || fallbackCalls.Load() != 1 {
		t.Fatalf("result=%+v error=%v fallback=%d", result, err, fallbackCalls.Load())
	}
}

func TestLLMPrimaryInvalidURLDoesNotEchoCredentials(t *testing.T) {
	for _, rawURL := range []string{
		"http://primary.invalid/v1",
		"https://user:primary-secret-marker@primary.invalid/v1",
		"https://primary.invalid/v1?api_key=primary-secret-marker",
		"https://primary.invalid/v1#primary-secret-marker",
		"https://primary.invalid/%zz?primary-secret-marker",
	} {
		_, err := NewLLMServiceWithConfig(LLMConfig{PrimaryBaseURL: rawURL, PrimaryModel: "primary-model", PrimaryAPIKey: "primary-secret-marker"})
		if err == nil {
			t.Fatal("invalid primary URL was accepted")
		}
		if strings.Contains(err.Error(), "primary-secret-marker") || strings.Contains(err.Error(), rawURL) {
			t.Fatal("invalid primary URL error disclosed its raw URL or credentials")
		}
	}
}

func TestLLMProviderSelectionErrorsDoNotEchoModelOutput(t *testing.T) {
	const rawResponse = `{"model-output-private-marker":"arbitrary response payload"}`
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePrimaryLLMResponse(w, rawResponse)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"response": rawResponse})
	}))
	defer fallback.Close()
	service := primarySecurityService(t, primary.URL, fallback.URL, nil)
	_, err := service.FuzzyMatchCardWithValidationContext(context.Background(), "Pikachu", []models.Card{{ID: "pikachu", Name: "Pikachu"}})
	if !errors.Is(err, ErrInvalidLLMResponse) {
		t.Fatalf("error=%v, want invalid model response", err)
	}
	if strings.Contains(err.Error(), "model-output-private-marker") || strings.Contains(err.Error(), "arbitrary response payload") {
		t.Fatal("selection validation exposed raw model response data")
	}
}
