package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"pokget/internal/service"
)

func priceCardRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "set_name", "set_code", "collector_number", "game", "language",
		"rarity", "variant", "source_id", "source_card_id", "price_usd", "price_eur",
	})
}

func TestAPICardPriceReturnsResolvedQuote(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery("SELECT id, name").WithArgs("card-1").
		WillReturnRows(priceCardRow().AddRow("card-1", "Charizard", "Base Set", "BS", "4", "pokemon", "en", "Rare", "normal", "tcgdex", "base1-4", 0, 0))
	mock.ExpectExec("UPDATE cards").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT currency FROM users").WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))

	h.Prices = service.NewPriceService(map[string][]service.PriceClient{
		"pokemon": {&service.MockPriceClient{FixedEUR: 12.5, FixedUSD: 14}},
	})

	recorder := httptest.NewRecorder()
	h.APICardPrice(recorder, authedRequest(t, http.MethodGet, "/api/cards/price?id=card-1", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["price"] != 12.5 || payload["currency"] != "EUR" {
		t.Fatalf("payload = %v, want price 12.5 in EUR", payload)
	}
	if payload["usd"] != 14.0 {
		t.Fatalf("usd = %v, want 14", payload["usd"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAPICardPriceReportsMissingCard(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery("SELECT id, name").WithArgs("missing").WillReturnError(sql.ErrNoRows)

	recorder := httptest.NewRecorder()
	h.APICardPrice(recorder, authedRequest(t, http.MethodGet, "/api/cards/price?id=missing", ""))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestAPICardPriceMarksUnavailableWithoutSourcePrice(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	mock.ExpectQuery("SELECT id, name").WithArgs("card-2").
		WillReturnRows(priceCardRow().AddRow("card-2", "Unknown", "Set", "S", "1", "pokemon", "en", "", "", "", "", 0, 0))
	mock.ExpectQuery("SELECT currency FROM users").WithArgs("test-user").
		WillReturnRows(sqlmock.NewRows([]string{"currency"}).AddRow("EUR"))

	h.Prices = service.NewPriceService(map[string][]service.PriceClient{
		"pokemon": {&service.MockPriceClient{}},
	})

	recorder := httptest.NewRecorder()
	h.APICardPrice(recorder, authedRequest(t, http.MethodGet, "/api/cards/price?id=card-2", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["unavailable"] != true {
		t.Fatalf("payload = %v, want unavailable=true", payload)
	}
}

func TestAPICardPriceRequiresID(t *testing.T) {
	recorder := httptest.NewRecorder()
	new(Handler).APICardPrice(recorder, authedRequest(t, http.MethodGet, "/api/cards/price", ""))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
