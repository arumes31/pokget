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
	"database/sql"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"pokget/internal/models"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func createDefaultTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	return img
}

func TestFingerprintService(t *testing.T) {
	s := NewFingerprintService()

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
		scraper.TCGPlayer = nil

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
		scraper.TCGPlayer = nil

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
		scraper.TCGPlayer = nil

		for _, game := range games {
			card := models.Card{Name: "N", Set: "S", Game: game}
			_, _, err := scraper.FetchPrice(card)
			if err != nil {
				t.Errorf("FetchPrice failed for game %s: %v", game, err)
			}
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
		mock.ExpectBegin()
		rows := sqlmock.NewRows([]string{"xp"}).AddRow(500)
		mock.ExpectQuery("UPDATE users SET xp = GREATEST\\(xp \\+ \\$1, 0\\) WHERE id = \\$2 RETURNING xp").
			WithArgs(400, "user-1").WillReturnRows(rows)
		mock.ExpectExec("UPDATE users SET rank_title = \\$1 WHERE id = \\$2 AND xp = \\$3").
			WithArgs("Card Scout", "user-1", 500).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

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
		mock.ExpectBegin()
		mock.ExpectQuery("UPDATE users SET xp = GREATEST\\(xp \\+ \\$1, 0\\) WHERE id = \\$2 RETURNING xp").
			WithArgs(100, "user-2").WillReturnError(sql.ErrNoRows)
		mock.ExpectRollback()

		_, _, err := s.AddXP("user-2", 100)
		if err == nil {
			t.Error("Expected error from AddXP when user not found")
		}
	})

	t.Run("AddXP_InvalidAmount", func(t *testing.T) {
		if _, _, err := s.AddXP("user-2", 0); err == nil {
			t.Error("Expected zero XP amount to be rejected")
		}
	})

	t.Run("RecordHeartbeat_Success", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("UPDATE users\\s+SET xp = xp \\+ \\$1, last_heartbeat_at").
			WithArgs(1, "hb-user-1").WillReturnRows(sqlmock.NewRows([]string{"xp"}).AddRow(12))
		mock.ExpectExec("UPDATE users SET rank_title").
			WithArgs("Novice Collector", "hb-user-1", 12).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		xp, rank, err := s.RecordHeartbeat("hb-user-1")
		if err != nil {
			t.Fatalf("RecordHeartbeat failed: %v", err)
		}
		if xp != 12 || rank != "Novice Collector" {
			t.Errorf("got xp=%d rank=%s, want 12 and Novice Collector", xp, rank)
		}
	})

	t.Run("RecordHeartbeat_RateLimited", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("UPDATE users\\s+SET xp = xp \\+ \\$1, last_heartbeat_at").
			WithArgs(1, "hb-user-2").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT COALESCE\\(xp, 0\\) FROM users WHERE id = \\$1").
			WithArgs("hb-user-2").WillReturnRows(sqlmock.NewRows([]string{"xp"}).AddRow(25))
		mock.ExpectRollback()

		xp, rank, err := s.RecordHeartbeat("hb-user-2")
		if err != nil {
			t.Fatalf("RecordHeartbeat failed: %v", err)
		}
		if xp != 25 {
			t.Errorf("got xp=%d, want unchanged 25", xp)
		}
		if rank != "Novice Collector" {
			t.Errorf("got rank=%s, want Novice Collector", rank)
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
