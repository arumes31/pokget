package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/models"
)

func TestGameHasCards(t *testing.T) {
	cards := []models.Card{
		{ID: "pokemon-card", Game: "pokemon"},
		{ID: "one-piece-card", Game: "One Piece"},
	}
	if !gameHasCards(cards, models.TCGPokemon) {
		t.Fatal("pokemon card not found")
	}
	if gameHasCards(cards, models.TCGMagic) {
		t.Fatal("magic reported without magic cards")
	}
	if !gameHasCards(cards, models.TCGOnePiece) {
		t.Fatal("legacy One Piece label was not normalized")
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
