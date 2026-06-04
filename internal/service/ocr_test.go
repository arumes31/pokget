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
	"database/sql"
	"image"
	"image/png"
	"pokget/internal/db"
	"pokget/internal/models"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestOCRMatchingLogic(t *testing.T) {
	mockCards := []models.Card{
		{Name: "Charizard"},
		{Name: "Pikachu"},
		{Name: "Mew"},
	}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"Exact match", "Pikachu", "Pikachu"},
		{"Case insensitive", "charizard", "Charizard"},
		{"Partial sentence", "Found a rare Charizard card today", "Charizard"},
		{"Word boundary - Mewtwo not Mew", "Mewtwo is here", "Unknown Card"},
		{"No match", "Bulbasaur", "Unknown Card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := "Unknown Card"
			for _, card := range mockCards {
				if containsIgnoreCase(tt.text, card.Name) {
					detected = card.Name
					break
				}
			}
			if detected != tt.expected {
				t.Errorf("For text '%s', expected %s, got %s", tt.text, tt.expected, detected)
			}
		})
	}
}

func containsIgnoreCase(s, substr string) bool {
	if substr == "" {
		return true
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(substr) + `\b`
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}

func TestProcessCardScan_WithDB(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer sqlDB.Close()

	// Save old DB and restore it after test
	oldDB := db.DB
	db.DB = sqlDB
	defer func() { db.DB = oldDB }()

	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	t.Run("DBMatch", func(t *testing.T) {
		mock.ExpectQuery("SELECT name FROM cards").
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("Pikachu"))

		_, card, _, err := ProcessCardScan(buf.Bytes(), nil, "eng", nil)
		if err != nil {
			t.Errorf("ProcessCardScan failed: %v", err)
		}
		if card != "Pikachu" {
			t.Errorf("Expected Pikachu from DB, got %s", card)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Expectations not met: %v", err)
		}
	})

	t.Run("DBNoMatch", func(t *testing.T) {
		mock.ExpectQuery("SELECT name FROM cards").
			WithArgs(sqlmock.AnyArg()).
			WillReturnError(sql.ErrNoRows)

		_, card, _, err := ProcessCardScan(buf.Bytes(), nil, "eng", nil)
		if err != nil {
			t.Errorf("ProcessCardScan failed: %v", err)
		}
		if card != "Unknown Card" {
			t.Errorf("Expected Unknown Card, got %s", card)
		}
	})
}

func TestProcessCardScan_Stub(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)

	text, card, _, err := ProcessCardScan(buf.Bytes(), nil, "", nil)
	if err != nil {
		t.Errorf("ProcessCardScan failed: %v", err)
	}
	// When CGO/Tesseract is enabled, it might return empty text for a blank image instead of the stub message.
	if !containsIgnoreCase(text, "OCR Not Available") && text != "" {
		t.Errorf("Unexpected OCR results: text=%s, card=%s", text, card)
	}
	if card != "Unknown Card" {
		t.Errorf("Expected Unknown Card, got %s", card)
	}
}
