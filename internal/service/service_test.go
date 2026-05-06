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
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"path/filepath"
	"pokget/internal/models"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-redis/redismock/v9"
)

func createTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	return img
}

func TestFingerprintService(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	defer db.Close()

	s := NewFingerprintService(db)

	t.Run("CalculateHash", func(t *testing.T) {
		img := createTestImage()
		hash, err := s.CalculateHash(img)
		if err != nil {
			t.Errorf("CalculateHash failed: %v", err)
		}
		if hash == 0 {
			t.Error("Expected non-zero hash")
		}
	})

	t.Run("MatchFingerprint", func(t *testing.T) {
		img := createTestImage()
		hash, _ := s.CalculateHash(img)

		rows := sqlmock.NewRows([]string{"id", "name", "set_name", "image_url", "phash"}).
			AddRow("test-id", "Test Card", "Test Set", "http://example.com/img.png", hash)

		mock.ExpectQuery("SELECT id, name, set_name, image_url, phash FROM cards").
			WillReturnRows(rows)

		card, distance, err := s.MatchFingerprint(hash)
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

	t.Run("MatchFingerprint_QueryError", func(t *testing.T) {
		mock.ExpectQuery("SELECT id").WillReturnError(sql.ErrConnDone)
		_, _, err := s.MatchFingerprint(0)
		if err == nil {
			t.Error("Expected error from MatchFingerprint when query fails")
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake-image-data"))
		}))
		defer server.Close()

		path, err := s.GetImagePath("test-card", server.URL)
		if err != nil {
			t.Errorf("GetImagePath failed: %v", err)
		}
		if filepath.Base(path) != "test-card.png" {
			t.Errorf("Expected filename test-card.png, got %s", filepath.Base(path))
		}

		// Verify file exists
		if _, err := os.Stat(path); err != nil {
			t.Error("File was not created")
		}

		// Second call should hit cache (server not used)
		path2, err := s.GetImagePath("test-card", "http://invalid-url")
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
			t.Error("Expected error for 500 response")
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

	t.Run("Log_Error", func(t *testing.T) {
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
