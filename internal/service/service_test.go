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
	"net/smtp"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-redis/redismock/v9"
)

func createDefaultTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	return img
}

func TestFingerprintService(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	s := NewFingerprintService(db)

	t.Run("CalculateHash", func(t *testing.T) {
		img := createDefaultTestImage()
		hash, err := s.CalculateHash(img)
		if err != nil {
			t.Errorf("CalculateHash failed: %v", err)
		}
		if hash == 0 {
			t.Error("Expected non-zero hash")
		}
	})

	t.Run("CalculateHash_Error", func(t *testing.T) {
		// Use a nil image to trigger error
		_, err := s.CalculateHash(nil)
		if err == nil {
			t.Error("Expected error for nil image")
		}
	})

	t.Run("MatchFingerprint", func(t *testing.T) {
		img := createDefaultTestImage()
		hash, _ := s.CalculateHash(img)

		cards := []models.Card{
			{ID: "test-id", Name: "Test Card", Set: "Test Set", ImageURL: "http://example.com/img.png", Phash: &hash},
		}

		card, distance, err := s.MatchFingerprint(hash, cards)
		if err != nil {
			t.Errorf("MatchFingerprint failed: %v", err)
		}
		if card == nil {
			t.Error("Expected to find a match")
		}
		if distance != 0 {
			t.Errorf("Expected distance 0, got %d", distance)
		}
	})

	t.Run("MatchFingerprint_NoMatch", func(t *testing.T) {
		_, _, err := s.MatchFingerprint(0, []models.Card{})
		if err != nil {
			t.Errorf("MatchFingerprint should not return error on empty list: %v", err)
		}
	})
}

func TestImageCacheService_Error(t *testing.T) {
	t.Run("InvalidDir", func(t *testing.T) {
		// Try to create in a path that should fail (e.g. nested in non-existent)
		// Or just a very long path/invalid characters on windows
		s := NewImageCacheService("Z:\\invalid\\path\\that\\does\\not\\exist")
		if s == nil {
			t.Error("Expected service instance even if mkdir fails")
		}
	})
}

func TestImageCacheService(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pokget-cache-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s := NewImageCacheService(tempDir)

	t.Run("DownloadAndCache", func(t *testing.T) {
		// SSRF validation: non-https scheme rejected
		_, err := s.GetImagePath("test-card", "http://example.com/image.png")
		if err == nil || !strings.Contains(err.Error(), "insecure protocol") {
			t.Errorf("Expected insecure protocol error, got: %v", err)
		}

		// SSRF validation: untrusted host rejected
		_, err = s.GetImagePath("test-card", "https://malicious.com/image.png")
		if err == nil || !strings.Contains(err.Error(), "untrusted host") {
			t.Errorf("Expected untrusted host error, got: %v", err)
		}

		// To test success, we create a file manually as if it was cached
		safeID := filepath.Base("test-card")
		path := filepath.Join(tempDir, safeID+".png")
		_ = os.WriteFile(path, []byte("fake-image-data"), 0600)

		// Call should hit cache (validation skipped for cached files)
		path2, err := s.GetImagePath("test-card", "https://example.com/any")
		if err != nil {
			t.Errorf("GetImagePath (cached) failed: %v", err)
		}
		if path != path2 {
			t.Error("Paths should be identical")
		}
	})

	t.Run("HTTPError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := s.GetImagePath("not-found", server.URL)
		if err == nil {
			t.Error("Expected error for 404 response")
		}
	})
}

func TestMailService(t *testing.T) {
	s := NewMailService()

	t.Run("SendConfirmationEmail", func(t *testing.T) {
		s.sendMailFunc = func(_ string, _ smtp.Auth, _ string, to []string, _ []byte) error {
			if to[0] != "test@example.com" {
				t.Errorf("Expected recipient test@example.com, got %s", to[0])
			}
			return nil
		}

		err := s.SendConfirmationEmail("test@example.com", "fake-token")
		if err != nil {
			t.Errorf("SendConfirmationEmail failed: %v", err)
		}
	})

	t.Run("SendConfirmationEmail_Error", func(t *testing.T) {
		// Without a valid SMTP host, SendConfirmationEmail should fail if sendMailFunc is smtp.SendMail
		os.Setenv("SMTP_HOST", "invalid-host")
		os.Setenv("SMTP_PORT", "25")
		os.Setenv("SMTP_USER", "user")
		os.Setenv("SMTP_PASS", "pass")
		defer os.Unsetenv("SMTP_HOST")

		svc := NewMailService()
		// Use real smtp.SendMail (which will fail)
		err := svc.SendConfirmationEmail("test@example.com", "token123")
		if err == nil {
			t.Error("Expected error when sending mail to invalid host")
		}
	})
}

func TestLLMService(t *testing.T) {
	s := NewLLMService()

	t.Run("FuzzyMatchCard_Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"response": "id-123"}`))
		}))
		defer server.Close()

		s.BaseURL = server.URL
		s.HTTPClient = server.Client()

		match, err := s.FuzzyMatchCard("Chrizard", []models.Card{{ID: "id-123", Name: "Charizard"}})
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
			t.Error("Expected error for 500 response")
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
		if !containsIgnoreCase(text, "OCR Not Available") {
			t.Errorf("Expected stub text, got %s", text)
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
		_, err := scraper.TCGPlayer.Scrape(card)
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
		scraper.Cardmarket.BaseURL = server.URL

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
		scraper.Cardmarket.BaseURL = server.URL

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
		scraper.Cardmarket.BaseURL = server.URL

		for _, game := range games {
			card := models.Card{Name: "N", Set: "S", Game: game}
			_, _, err := scraper.FetchPrice(card)
			if err != nil {
				t.Errorf("FetchPrice failed for game %s: %v", game, err)
			}
		}
	})
}

func TestCryptoService(t *testing.T) {
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
		// BUG-C02 FIX: AddXP now uses atomic UPDATE...RETURNING instead of SELECT+UPDATE
		// The placeholder rank_title is GetUserRank(0).Title = "Novice Collector"
		rows := sqlmock.NewRows([]string{"xp", "rank_title"}).AddRow(500, "Novice Collector")
		mock.ExpectQuery("UPDATE users SET xp = xp \\+ \\$1, rank_title = \\$2 WHERE id = \\$3 RETURNING xp, rank_title").
			WithArgs(400, "Novice Collector", "user-1").WillReturnRows(rows)

		// Rank changed from "Novice Collector" to "Card Scout", so a follow-up UPDATE is needed
		mock.ExpectExec("UPDATE users SET rank_title = \\$1 WHERE id = \\$2").
			WithArgs("Card Scout", "user-1").WillReturnResult(sqlmock.NewResult(1, 1))

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
		// BUG-C02 FIX: AddXP now uses UPDATE...RETURNING, so error comes from that query
		mock.ExpectQuery("UPDATE users SET xp = xp \\+ \\$1, rank_title = \\$2 WHERE id = \\$3 RETURNING xp, rank_title").
			WithArgs(100, "Novice Collector", "user-2").WillReturnError(sql.ErrNoRows)

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
		ch, unsubscribe := bus.Subscribe("test-event")
		defer unsubscribe()

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
