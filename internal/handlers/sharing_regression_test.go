package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
)

func TestPublicVaultKeepsItemsWithNullableGradingFields(t *testing.T) {
	t.Parallel()

	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create SQL mock: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	templates := template.Must(template.New("public_vault.html").Parse(
		`{{define "public_vault.html"}}{{len .Portfolio}}{{end}}`,
	))
	handler := &Handler{Templates: templates, DB: database}

	mock.ExpectQuery("SELECT id, email").
		WithArgs("public-user").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "rank_title", "xp", "currency"}).
			AddRow("user-1", "collector@example.com", "Collector", 100, "EUR"))
	mock.ExpectQuery("SELECT p.id").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "condition", "format", "grade", "grading_company", "notes",
			"name", "set_name", "price_usd", "price_eur", "image_url", "game",
		}).AddRow(
			"item-1", "Near Mint", "Raw", nil, nil, nil,
			"Test Card", "Test Set", "10.00", "9.00", "/card.png", "pokemon",
		))

	request := httptest.NewRequest(http.MethodGet, "/vault/public-user", nil)
	request = mux.SetURLVars(request, map[string]string{"slug": "public-user"})
	response := httptest.NewRecorder()

	handler.PublicVault(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if response.Body.String() != "1" {
		t.Fatalf("public item count = %q, want 1", response.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
