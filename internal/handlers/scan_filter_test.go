package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/models"
)

func TestFilterCardsByGame(t *testing.T) {
	cards := []models.Card{
		{ID: "pokemon-card", Game: "pokemon"},
		{ID: "magic-card", Game: "magic"},
		{ID: "one-piece-card", Game: "One Piece"},
	}

	filtered, ok := filterCardsByGame(cards, "PoKeMoN")
	if !ok || len(filtered) != 1 || filtered[0].ID != "pokemon-card" {
		t.Fatalf("unexpected filter result: ok=%v cards=%+v", ok, filtered)
	}
	if _, ok := filterCardsByGame(cards, "unknown"); ok {
		t.Fatal("unsupported game was accepted")
	}
	onePiece, ok := filterCardsByGame(cards, "one-piece")
	if !ok || len(onePiece) != 1 || onePiece[0].ID != "one-piece-card" {
		t.Fatalf("legacy One Piece label was not normalized: ok=%v cards=%+v", ok, onePiece)
	}
}

func TestAPIScanRejectsEmptySelectedTCGCorpus(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("card_image", "card.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("unused image body")); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("game", "lorcana"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	handler := &Handler{MockCards: []models.Card{{ID: "pokemon-card", Game: "pokemon"}}}
	request := httptest.NewRequest(http.MethodPost, "/api/scan", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()

	handler.APIScan(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}
