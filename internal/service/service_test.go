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
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"pokget/internal/models"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-redis/redismock/v9"
)

func TestLLMService(t *testing.T) {
	s := NewLLMService()

	t.Run("FuzzyMatchCard_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"response": "Charizard"}`))
		}))
		defer server.Close()

		s.BaseURL = server.URL
		s.HTTPClient = server.Client()

		match, err := s.FuzzyMatchCard("Chrizard", []models.Card{{Name: "Charizard"}})
		if err != nil {
			t.Errorf("FuzzyMatchCard failed: %v", err)
		}
		if match != "Charizard" {
			t.Errorf("Expected Charizard, got %s", match)
		}
	})

	t.Run("FuzzyMatchCard_HTTPError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		s.BaseURL = server.URL
		s.HTTPClient = server.Client()

		_, err := s.FuzzyMatchCard("Chrizard", []models.Card{{Name: "Charizard"}})
		if err == nil {
			t.Error("Expected error for HTTP 500 response")
		}
	})

	t.Run("FuzzyMatchCard_MalformedJSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json`))
		}))
		defer server.Close()

		s.BaseURL = server.URL
		s.HTTPClient = server.Client()

		_, err := s.FuzzyMatchCard("Chrizard", []models.Card{{Name: "Charizard"}})
		if err == nil {
			t.Error("Expected error for malformed JSON")
		}
	})
}

func TestProcessCardScan_Full(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	t.Run("Match", func(t *testing.T) {
		cards := []models.Card{{ID: "1", Name: "Charizard"}}
		text, card, _, err := ProcessCardScan(buf.Bytes(), cards, "", nil)
		if err != nil {
			t.Errorf("ProcessCardScan failed: %v", err)
		}
		if card == "" {
			t.Error("Expected to find a match")
		}
		// When CGO/Tesseract is enabled, it might return empty text for a blank image instead of the stub message.
		if !containsIgnoreCase(text, "OCR Not Available") && text != "" {
			t.Errorf("Unexpected OCR results: text=%s, card=%s", text, card)
		}
	})
}

func TestScraperPriceClient(t *testing.T) {
	t.Run("DefaultClient_Nil", func(t *testing.T) {
		client := &DefaultPriceClient{Scraper: nil}
		_, _, err := client.FetchPrice(models.Card{})
		if err == nil {
			t.Error("Expected error for nil scraper")
		}
	})

	t.Run("ScrapeError", func(t *testing.T) {
		scraper := &ScraperPriceClient{}

		card := models.Card{Name: "MissingNo", Set: "Glitch"}
		_, _, err := scraper.FetchPrice(card)
		// Should return an error because it fails to connect/find the actual URL or parse
		if err == nil {
			t.Error("Expected scrape error for MissingNo")
		}
	})

	t.Run("FetchPriceHeadless_Unsupported", func(t *testing.T) {
		scraper := &ScraperPriceClient{}
		card := models.Card{Name: "Pikachu", Game: "Pokemon"} // Headless only supports Magic/YuGiOh
		_, err := scraper.fetchPriceHeadless(card)
		if err == nil {
			t.Error("Expected error for unsupported game")
		}
	})

	t.Run("FetchPrice_ScrapeSuccess", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Match .price-container .color-primary
			_, _ = w.Write([]byte(`<div class="price-container"><span class="color-primary">12,34 €</span></div>`))
		}))
		defer server.Close()

		scraper := NewScraperPriceClient()
		scraper.BaseURL = server.URL

		card := models.Card{Name: "Charizard", Set: "Base", Game: "Pokemon"}
		_, eur, err := scraper.FetchPrice(card)
		if err != nil {
			t.Errorf("FetchPrice failed: %v", err)
		}
		if eur != 12.34 {
			t.Errorf("Expected 12.34, got %f", eur)
		}
	})

	t.Run("FetchPrice_ParseError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<div class="price-container"><span class="color-primary">invalid</span></div>`))
		}))
		defer server.Close()

		scraper := NewScraperPriceClient()
		scraper.BaseURL = server.URL

		card := models.Card{Name: "Charizard", Set: "Base"}
		_, _, err := scraper.FetchPrice(card)
		if err == nil {
			t.Error("Expected parse error")
		}
	})

	t.Run("FetchPrice_GameBranches", func(t *testing.T) {
		games := []string{"One Piece", "Lorcana", "Weiss Schwarz", "Magic", "mtg"}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<div class="price-container"><span class="color-primary">10,00 €</span></div>`))
		}))
		defer server.Close()

		scraper := NewScraperPriceClient()
		scraper.BaseURL = server.URL

		for _, game := range games {
			card := models.Card{Name: "N", Set: "S", Game: game}
			_, _, err := scraper.FetchPrice(card)
			if err != nil {
				t.Errorf("FetchPrice failed for game %s: %v", game, err)
			}
		}
	})
}

func TestAuditService(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	s := NewAuditService(db)

	t.Run("Log_Success", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WithArgs("user-1", "LOGIN", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		s.Log("user-1", "LOGIN", map[string]interface{}{"ip": "1.2.3.4"})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("Log_Error", func(_ *testing.T) {
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(sql.ErrConnDone)
		// Should not panic, just log the error
		s.Log("user-1", "LOGIN", nil)
	})
}

func TestCryptoService(t *testing.T) {
	t.Run("New_Error", func(t *testing.T) {
		_, err := NewCryptoService("too-short")
		if err == nil {
			t.Error("Expected error for short key")
		}
	})

	key := "12345678901234567890123456789012" // 32 bytes
	s, err := NewCryptoService(key)
	if err != nil {
		t.Fatalf("Failed to create CryptoService: %v", err)
	}

	t.Run("Encrypt_Error", func(t *testing.T) {
		// Mocking cipher.AEAD is hard, but we can hit some branches
		_, err := s.Encrypt("") // empty string is fine
		if err != nil {
			t.Errorf("Encrypt failed for empty string: %v", err)
		}
	})

	t.Run("EncryptDecrypt", func(t *testing.T) {
		plaintext := "Secret Data"
		ciphertext, err := s.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		decrypted, err := s.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("Expected %s, got %s", plaintext, decrypted)
		}
	})

	t.Run("Decrypt_Invalid", func(t *testing.T) {
		_, err := s.Decrypt("invalid-base64")
		if err == nil {
			t.Error("Expected error for invalid base64")
		}

		_, err = s.Decrypt("YWJjZA==") // "abcd" in base64, too short for AES-GCM nonce
		if err == nil {
			t.Error("Expected error for too short ciphertext")
		}
	})
}

func TestGamificationService(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	s := NewGamificationService(db)

	t.Run("AddXP_Success", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"xp", "rank_title"}).AddRow(100, "Novice Collector")
		mock.ExpectQuery("SELECT xp, rank_title FROM users WHERE id = \\$1").WithArgs("user-1").WillReturnRows(rows)

		// 100 + 400 = 500 -> "Card Scout"
		mock.ExpectExec("UPDATE users SET xp = \\$1, rank_title = \\$2 WHERE id = \\$3").
			WithArgs(500, "Card Scout", "user-1").WillReturnResult(sqlmock.NewResult(1, 1))

		newXP, newRank, err := s.AddXP("user-1", 400)
		if err != nil {
			t.Errorf("AddXP failed: %v", err)
		}
		if newXP != 500 {
			t.Errorf("Expected 500 XP, got %d", newXP)
		}
		if newRank != "Card Scout" {
			t.Errorf("Expected Card Scout, got %s", newRank)
		}
	})

	t.Run("AddXP_QueryError", func(t *testing.T) {
		mock.ExpectQuery("SELECT xp, rank_title FROM users WHERE id = \\$1").WithArgs("user-2").WillReturnError(sql.ErrNoRows)

		_, _, err := s.AddXP("user-2", 100)
		if err == nil {
			t.Error("Expected error from AddXP when user not found")
		}
	})

	t.Run("GetUserRank", func(t *testing.T) {
		rank := s.GetUserRank(1600)
		if rank.Title != "Hobbyist" {
			t.Errorf("Expected Hobbyist, got %s", rank.Title)
		}
	})

	t.Run("GetProgressToNextRank", func(t *testing.T) {
		relXP, reqXP, pct := s.GetProgressToNextRank(600)
		// 600 - 500 = 100 rel
		// 1500 - 500 = 1000 req
		// pct = 10%
		if relXP != 100 {
			t.Errorf("Expected 100 relXP, got %d", relXP)
		}
		if reqXP != 1000 {
			t.Errorf("Expected 1000 reqXP, got %d", reqXP)
		}
		if pct != 10.0 {
			t.Errorf("Expected 10.0 pct, got %f", pct)
		}
	})

	t.Run("GetProgressToNextRank_MaxRank", func(t *testing.T) {
		relXP, reqXP, pct := s.GetProgressToNextRank(300000)
		if relXP != 300000 || reqXP != 300000 || pct != 100.0 {
			t.Errorf("Expected max rank behavior, got %d, %d, %f", relXP, reqXP, pct)
		}
	})
}

func TestCacheService(t *testing.T) {
	ctx := context.Background()
	db, mock := redismock.NewClientMock()

	s := &CacheService{client: db}

	t.Run("SetGet", func(t *testing.T) {
		val := map[string]string{"foo": "bar"}
		data, _ := json.Marshal(val)

		mock.ExpectSet("test-key", data, 0).SetVal("OK")
		err := s.Set(ctx, "test-key", val, 0)
		if err != nil {
			t.Errorf("Set failed: %v", err)
		}

		mock.ExpectGet("test-key").SetVal(string(data))
		var got map[string]string
		err = s.Get(ctx, "test-key", &got)
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if got["foo"] != "bar" {
			t.Errorf("Expected bar, got %s", got["foo"])
		}
	})

	t.Run("Delete", func(t *testing.T) {
		mock.ExpectDel("test-key").SetVal(1)
		err := s.Delete(ctx, "test-key")
		if err != nil {
			t.Errorf("Delete failed: %v", err)
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

	t.Run("NewCacheService", func(t *testing.T) {
		os.Setenv("REDIS_URL", "localhost:6379")
		defer os.Unsetenv("REDIS_URL")
		s := NewCacheService()
		if s == nil {
			t.Error("Expected CacheService instance")
		}
	})
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
		}
	}
}

func TestEventBus(t *testing.T) {
	bus := NewEventBus()

	t.Run("SubscribePublish", func(t *testing.T) {
		ch := bus.Subscribe("test-event")

		bus.Publish(Event{Type: "test-event", Payload: "hello"})

		select {
		case ev := <-ch:
			if ev.Payload != "hello" {
				t.Errorf("Expected hello, got %v", ev.Payload)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("Timed out waiting for event")
		}
	})
}
