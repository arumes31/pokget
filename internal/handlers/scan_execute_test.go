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
	"strings"
	"testing"

	"pokget/internal/claudedetect"
	"pokget/internal/models"
	"pokget/internal/service"
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

// stubScanDetector answers every scan with the same catalog card and supports
// the magnified crops the pipeline sends.
type stubScanDetector struct{}

func (stubScanDetector) Detect(context.Context, []byte, string, string) (*claudedetect.DetectedCardInfo, error) {
	return &claudedetect.DetectedCardInfo{
		Name:            "Pikachu",
		CollectorNumber: "025",
		SetCode:         "SV1",
		Language:        "en",
		Confidence:      0.97,
	}, nil
}

func (d stubScanDetector) DetectImages(context.Context, [][]byte, string, string) (*claudedetect.DetectedCardInfo, error) {
	return d.Detect(nil, nil, "", "")
}

func (stubScanDetector) Model() string             { return "claude-sonnet-5-5" }
func (stubScanDetector) PromptCachingEnabled() bool { return true }

func TestExecuteScanWithoutDetectionPipelineFailsClearly(t *testing.T) {
	// Recognition is server-side only. A deployment without the pipeline must
	// refuse the scan instead of returning a guessed card.
	h := &Handler{MockCards: []models.Card{{ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en"}}}
	rr := httptest.NewRecorder()

	h.executeScan(rr, scanRequest(t, testCardPNG(t), nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not configured") {
		t.Errorf("expected a configuration error, got %q", rr.Body.String())
	}
}

func TestExecuteScanReturnsScopedMatchWithDiagnostics(t *testing.T) {
	card := models.Card{
		ID: "card-1", Name: "Pikachu", Game: "pokemon", Language: "en",
		SetCode: "SV1", Set: "Scarlet & Violet", CollectorNumber: "025",
		ImageURL: "/img/pikachu.png",
	}
	pipeline := service.NewDetectionPipeline(nil, nil)
	pipeline.Claude = stubScanDetector{}
	h := &Handler{MockCards: []models.Card{card}, Detection: pipeline}

	rr := httptest.NewRecorder()
	h.executeScan(rr, scanRequest(t, testCardPNG(t), map[string]string{
		"game":        "pokemon",
		"lang":        "en",
		"diagnostics": "true",
	}))

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
	matches, ok := resp["top_matches"].([]interface{})
	if !ok || len(matches) == 0 {
		t.Fatalf("expected top matches, got %v", resp["top_matches"])
	}
	first := matches[0].(map[string]interface{})
	for _, entry := range []map[string]interface{}{resp, first} {
		if entry["set"] != card.Set || entry["collector_number"] != card.CollectorNumber || entry["language"] != card.Language {
			t.Errorf("scan response omitted printing identity: %v", entry)
		}
	}
	if _, ok := resp["pipeline_metrics"]; !ok {
		t.Errorf("expected pipeline metrics in diagnostics response, got %v", resp)
	}
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
		req := scanRequest(t, imgBytes, map[string]string{"game": "unsupported_tcg"})
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