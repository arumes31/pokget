package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"pokget/internal/auth"
	"pokget/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func TestAPIScanDeviceText(t *testing.T) {
	h := &Handler{MockCards: []models.Card{{ID: "furret", Name: "Furret", Game: "pokemon", Language: "en", CollectorNumber: "136"}}}
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"valid", `{"ocr_text":"Furret 136/197","game":"pokemon","lang":"eng"}`, 200},
		{"unknown fields", `{"ocr_text":"Furret","game":"pokemon","lang":"eng","card_id":"forged"}`, 400},
		{"missing scope", `{"ocr_text":"Furret"}`, 400},
		{"empty", `{"ocr_text":"","game":"pokemon","lang":"eng"}`, 400},
		{"invalid json", `not json`, 400},
		{"trailing value", `{"ocr_text":"Furret","game":"pokemon","lang":"eng"}{}`, 400},
		{"wrong language", `{"ocr_text":"Furret","game":"pokemon","lang":"deu"}`, 422},
		{"oversized", `{"ocr_text":"` + strings.Repeat("x", int(maxTextScanRequestBytes)) + `"}`, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.APIScan(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body)
			}
			if tc.status == 200 {
				var response map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response["id"] != "furret" || response["needs_review"] != true || response["requires_image"] != false {
					t.Fatalf("response = %+v", response)
				}
			}
		})
	}
}

func TestTextScanCurrencyOutlivesDetectionBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		mock.ExpectQuery("SELECT currency FROM users WHERE id = \\$1").WithArgs("user-1").
			WillDelayFor(2 * time.Second).
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("USD"))
		h := &Handler{DB: database, ScanTimeout: time.Second, MockCards: []models.Card{{
			ID: "furret", Name: "Furret", Game: "pokemon", Language: "en", CollectorNumber: "136",
			PriceEUR: decimal.NewFromInt(10), PriceUSD: decimal.NewFromInt(12),
		}}}
		r := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader(`{"ocr_text":"Furret 136/197","game":"pokemon","lang":"eng"}`))
		r = r.WithContext(context.WithValue(r.Context(), auth.UserContextKey{}, "user-1"))
		w := httptest.NewRecorder()
		h.executeTextScan(w, r)
		var response struct {
			Price float64 `json:"price"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || response.Price != 12 {
			t.Fatalf("currency lookup consumed detection budget: %d %s", w.Code, w.Body)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
