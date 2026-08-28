package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

// testCardPNG renders a small synthetic card image for scan tests.
func testCardPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

// phashForImage computes the perceptual hash a FingerprintService would assign
// to the given image bytes, so tests can plant a matching card record.
func phashForImage(t *testing.T, fp *service.FingerprintService, imgBytes []byte) int64 {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		t.Fatalf("decode test PNG: %v", err)
	}
	hash, err := fp.CalculateHash(img)
	if err != nil {
		t.Fatalf("calculate phash: %v", err)
	}
	return hash
}

// scanRequest builds a multipart scan request carrying the given image bytes
// and extra form fields.
func scanRequest(t *testing.T, imgBytes []byte, fields map[string]string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if imgBytes != nil {
		part, err := writer.CreateFormFile("card_image", "card.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(imgBytes); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write form field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/scan", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestExecuteScan_FingerprintMatch(t *testing.T) {
	imgBytes := testCardPNG(t)
	fp := service.NewFingerprintService(nil)
	hash := phashForImage(t, fp, imgBytes)

	card := models.Card{
		ID: "card-1", Name: "Pikachu", Phash: &hash,
		PriceUSD: decimal.NewFromInt(14), PriceEUR: decimal.NewFromInt(12),
		ImageURL: "/img/pikachu.png",
	}

	t.Run("DefaultEUR", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{card}, Fingerprint: fp}
		req := scanRequest(t, imgBytes, nil)
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scan response: %v", err)
		}
		if resp["detected"] != "Pikachu" || resp["id"] != "card-1" {
			t.Errorf("unexpected detection payload: %v", resp)
		}
		if resp["price"] != 12.0 {
			t.Errorf("expected EUR price 12, got %v", resp["price"])
		}
		if resp["image_url"] != "/img/pikachu.png" {
			t.Errorf("expected image URL, got %v", resp["image_url"])
		}
	})

	t.Run("UserCurrencyUSD", func(t *testing.T) {
		dbMock, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("create SQL mock: %v", err)
		}
		defer func() { _ = dbMock.Close() }()

		h := &Handler{MockCards: []models.Card{card}, Fingerprint: fp, DB: dbMock}
		req := scanRequest(t, imgBytes, nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey{}, "test-user"))
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT currency").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("USD"))

		h.executeScan(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scan response: %v", err)
		}
		if resp["price"] != 14.0 {
			t.Errorf("expected USD price 14, got %v", resp["price"])
		}
	})
}

func TestExecuteScan_OCRUnavailable(t *testing.T) {
	// No fingerprint match (card carries no phash) forces the OCR fallback.
	// Inject the failure so the contract is deterministic in both CGO/Tesseract
	// builds and the portable stub build.
	imgBytes := testCardPNG(t)
	card := models.Card{ID: "card-1", Name: "Pikachu"}

	h := &Handler{
		MockCards:   []models.Card{card},
		Fingerprint: service.NewFingerprintService(nil),
		scanProcessor: func(
			context.Context,
			[]byte,
			[]models.Card,
			string,
			*service.LLMService,
		) (string, string, []byte, error) {
			return "", "", nil, &service.OCRUnavailableError{Reason: "test"}
		},
	}
	req := scanRequest(t, imgBytes, nil)
	rr := httptest.NewRecorder()

	h.executeScan(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}
}

func TestExecuteScan_DetectionPipeline(t *testing.T) {
	imgBytes := testCardPNG(t)
	fp := service.NewFingerprintService(nil)
	hash := phashForImage(t, fp, imgBytes)

	t.Run("LegacyMatch", func(t *testing.T) {
		card := models.Card{
			ID: "card-1", Name: "Pikachu", Phash: &hash,
			PriceEUR: decimal.NewFromInt(12), ImageURL: "/img/pikachu.png",
		}
		h := &Handler{
			MockCards: []models.Card{card},
			Detection: service.NewDetectionPipeline(fp, nil),
		}
		req := scanRequest(t, imgBytes, nil)
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scan response: %v", err)
		}
		if resp["detected"] != "Pikachu" || resp["id"] != "card-1" {
			t.Errorf("unexpected detection payload: %v", resp)
		}
		if resp["confidence"] != 100.0 {
			t.Errorf("expected exact-match confidence 100, got %v", resp["confidence"])
		}
		if resp["needs_review"] != false {
			t.Errorf("expected needs_review false, got %v", resp["needs_review"])
		}
		matches, ok := resp["top_matches"].([]interface{})
		if !ok || len(matches) != 1 {
			t.Fatalf("expected one top match, got %v", resp["top_matches"])
		}
		first := matches[0].(map[string]interface{})
		if first["name"] != "Pikachu" || first["price"] != 12.0 {
			t.Errorf("unexpected top match entry: %v", first)
		}
	})

	t.Run("ScopedMatchWithDiagnostics", func(t *testing.T) {
		card := models.Card{
			ID: "card-1", Name: "Pikachu", Phash: &hash,
			Game: "pokemon", Language: "en",
			PriceEUR: decimal.NewFromInt(12), ImageURL: "/img/pikachu.png",
		}
		h := &Handler{
			MockCards: []models.Card{card},
			Detection: service.NewDetectionPipeline(fp, nil),
		}
		req := scanRequest(t, imgBytes, map[string]string{
			"game":        "pokemon",
			"lang":        "en",
			"diagnostics": "true",
		})
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scan response: %v", err)
		}
		if resp["detected"] != "Pikachu" {
			t.Errorf("unexpected detection payload: %v", resp)
		}
		if _, ok := resp["pipeline_metrics"]; !ok {
			t.Errorf("expected pipeline metrics in diagnostics response, got %v", resp)
		}
	})
}

func TestExecuteScan_RequestValidation(t *testing.T) {
	imgBytes := testCardPNG(t)
	pokemonCard := models.Card{ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en"}

	t.Run("MissingFile", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{pokemonCard}}
		req := scanRequest(t, nil, map[string]string{"game": "pokemon"})
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("EmptyImage", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{pokemonCard}}
		req := scanRequest(t, []byte{}, nil)
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Empty image") {
			t.Errorf("Expected empty image message, got %q", rr.Body.String())
		}
	})

	t.Run("UnsupportedGame", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{pokemonCard}}
		req := scanRequest(t, imgBytes, map[string]string{"game": "digimon"})
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Unsupported TCG") {
			t.Errorf("Expected unsupported TCG message, got %q", rr.Body.String())
		}
	})

	t.Run("UnsupportedLanguage", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{pokemonCard}}
		req := scanRequest(t, imgBytes, map[string]string{"game": "pokemon", "lang": "klingon"})
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Unsupported card language") {
			t.Errorf("Expected unsupported language message, got %q", rr.Body.String())
		}
	})

	t.Run("CanceledContext", func(t *testing.T) {
		h := &Handler{MockCards: []models.Card{pokemonCard}}
		req := scanRequest(t, imgBytes, nil)
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.executeScan(rr, req)

		if rr.Code != http.StatusRequestTimeout {
			t.Errorf("Expected status 408, got %d", rr.Code)
		}
	})
}
