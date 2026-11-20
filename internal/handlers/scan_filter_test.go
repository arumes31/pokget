package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokget/internal/models"
)

func TestCardsForGame(t *testing.T) {
	cards := []models.Card{
		{ID: "pokemon-card", Game: "pokemon"},
		{ID: "magic-card", Game: "magic"},
		{ID: "one-piece-card", Game: "One Piece"},
	}
	if got := cardsForGame(cards, models.TCGPokemon); len(got) != 1 || got[0].ID != "pokemon-card" {
		t.Fatalf("pokemon cards = %+v", got)
	}
	if got := cardsForGame(cards, models.TCGLorcana); len(got) != 0 {
		t.Fatalf("lorcana cards = %+v, want none", got)
	}
	if got := cardsForGame(cards, models.TCGOnePiece); len(got) != 1 || got[0].ID != "one-piece-card" {
		t.Fatalf("legacy One Piece label was not normalized: %+v", got)
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
