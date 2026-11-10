package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"pokget/internal/models"
)

func expectSyntheticPersist(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO catalog_sources").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO cards").WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestPersistSyntheticCardAppendsEachIDOnce(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	card := models.Card{Name: "Pikachu", Set: "Base"}
	card.ID = generateSyntheticCardID(card)
	before := len(h.MockCards)
	for range 3 {
		expectSyntheticPersist(mock)
		h.persistSyntheticCard(context.Background(), card)
	}
	if got := len(h.MockCards) - before; got != 1 {
		t.Fatalf("repeated persist grew the cache by %d entries, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistSyntheticCardRespectsCacheBudget(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	h.syntheticCached = make(map[string]struct{}, maxSyntheticCacheAppends)
	for i := range maxSyntheticCacheAppends {
		h.syntheticCached[fmt.Sprintf("card_%d", i)] = struct{}{}
	}
	before := len(h.MockCards)
	expectSyntheticPersist(mock)
	h.persistSyntheticCard(context.Background(), models.Card{ID: "card_new", Name: "Over budget"})
	if len(h.MockCards) != before {
		t.Fatal("cache grew past maxSyntheticCacheAppends")
	}
	// The row is still written, so the card stays usable and returns on reload.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddCardToPortfolioRejectsOversizedSyntheticFields(t *testing.T) {
	cases := map[string]url.Values{
		"card_name":        {"card_name": {strings.Repeat("x", maxSyntheticNameRunes+1)}},
		"set_name":         {"card_name": {"ok"}, "set_name": {strings.Repeat("x", maxSyntheticSetRunes+1)}},
		"collector_number": {"card_name": {"ok"}, "collector_number": {strings.Repeat("1", maxSyntheticNumberRunes+1)}},
		"language":         {"card_name": {"ok"}, "language": {strings.Repeat("e", maxSyntheticLangRunes+1)}},
		"game":             {"card_name": {"ok"}, "game": {strings.Repeat("g", maxSyntheticGameRunes+1)}},
		"fallback branch":  {"card_id": {"missing"}, "card_name": {strings.Repeat("x", maxSyntheticNameRunes+1)}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			if form.Get("card_id") != "" {
				mock.ExpectQuery("SELECT EXISTS").WithArgs("missing").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			}
			before := len(h.MockCards)
			rr := httptest.NewRecorder()
			h.AddCardToPortfolio(rr, authedRequest(t, http.MethodPost, "/portfolio/add", form.Encode()))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			if len(h.MockCards) != before {
				t.Fatal("oversized card reached the cache")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddCardToPortfolioDerivesSyntheticIDForUnknownCardID(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	form := url.Values{"card_id": {"attacker-chosen"}, "card_name": {"Mew"}, "set_name": {"Promo"}}
	derived := generateSyntheticCardID(models.Card{Name: "Mew\x00attacker-chosen", Set: "Promo"})
	if derived == generateSyntheticCardID(models.Card{Name: "Mew\x00other-printing", Set: "Promo"}) {
		t.Fatal("different missing catalog ids collapse into one synthetic card")
	}

	mock.ExpectQuery("SELECT EXISTS").WithArgs("attacker-chosen").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("INSERT INTO catalog_sources").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO cards").WithArgs(derived, "Mew", "Promo", "", "", "", "", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM binders").WithArgs("test-user").
		WillReturnError(context.Canceled)

	rr := httptest.NewRecorder()
	h.AddCardToPortfolio(rr, authedRequest(t, http.MethodPost, "/portfolio/add", form.Encode()))
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("synthetic card was not persisted under its derived id: %v", err)
	}
}

func TestAddCardToPortfolioRejectsOversizedFallbackCardID(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	long := strings.Repeat("i", maxSyntheticCardIDRunes+1)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(long).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	rr := httptest.NewRecorder()
	form := url.Values{"card_id": {long}, "card_name": {"Mew"}}
	h.AddCardToPortfolio(rr, authedRequest(t, http.MethodPost, "/portfolio/add", form.Encode()))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
