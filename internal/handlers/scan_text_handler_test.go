package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokget/internal/models"
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
