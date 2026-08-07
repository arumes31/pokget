package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestValidateRuntimeSchema(t *testing.T) {
	t.Run("complete schema", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()

		mock.ExpectQuery("WITH required").WillReturnRows(
			sqlmock.NewRows([]string{"missing"}).AddRow(nil),
		)

		if err := ValidateRuntimeSchema(context.Background(), database); err != nil {
			t.Fatalf("ValidateRuntimeSchema() error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("missing required objects", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()

		mock.ExpectQuery("WITH required").WillReturnRows(
			sqlmock.NewRows([]string{"missing"}).AddRow("cards.rarity, price_alerts.id"),
		)

		err = ValidateRuntimeSchema(context.Background(), database)
		if err == nil || !strings.Contains(err.Error(), "cards.rarity, price_alerts.id") {
			t.Fatalf("ValidateRuntimeSchema() error = %v, want missing schema objects", err)
		}
	})

	t.Run("query failure", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()

		mock.ExpectQuery("WITH required").WillReturnError(errors.New("database unavailable"))

		if err := ValidateRuntimeSchema(context.Background(), database); err == nil || !strings.Contains(err.Error(), "database unavailable") {
			t.Fatalf("ValidateRuntimeSchema() error = %v, want query failure", err)
		}
	})

	t.Run("nil database", func(t *testing.T) {
		if err := ValidateRuntimeSchema(context.Background(), nil); err == nil {
			t.Fatal("ValidateRuntimeSchema() error = nil, want uninitialized database error")
		}
	})
}
