package worker

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestSetAliasKeysUseTheMarketplaceSpelling(t *testing.T) {
	codeKeys, nameKeys := setAliasKeys("universus", "TMNT01", "045")
	if len(codeKeys) == 0 || len(nameKeys) == 0 {
		t.Fatalf("alias keys = %v / %v, want both indexes", codeKeys, nameKeys)
	}
	wantCode := priceMatchKeys("TMNT", "045")
	found := false
	for _, key := range codeKeys {
		if key == wantCode[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("code keys = %v, want %v", codeKeys, wantCode)
	}
	// The lookup is case-insensitive in the game and the set code.
	if other, _ := setAliasKeys("UniVersus", " tmnt01 ", "045"); len(other) != len(codeKeys) {
		t.Fatalf("alias lookup is not case-insensitive: %v vs %v", other, codeKeys)
	}
	if code, name := setAliasKeys("pokemon", "SV3", "089"); code != nil || name != nil {
		t.Fatalf("unconfigured set returned keys %v / %v", code, name)
	}
	// A group without an abbreviation only publishes its name.
	if code, name := setAliasKeys("universus", "FT01", "001"); code != nil || len(name) == 0 {
		t.Fatalf("FT01 = %v / %v, want a name alias only", code, name)
	}
}

func TestKnownSetAliasesCountsPerGame(t *testing.T) {
	if knownSetAliases("universus") == 0 {
		t.Fatal("universus alias table is empty")
	}
	if knownSetAliases("pokemon") != 0 {
		t.Fatal("pokemon has no aliases configured")
	}
}

// A card whose catalog set code never appears in the marketplace is matched through
// the configured alias.
func TestApplyFuzzyPricesUsesTheSetAlias(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-1", "TMNT01", "TMNT01", "045", "Leonardo")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code").WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-1", 4.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byCode := fuzzyIndex("TMNT", "045", 4.0)
	changed, err := worker.applyFuzzyPrices(context.Background(), "universus", nil, byCode, nil)
	if err != nil {
		t.Fatalf("applyFuzzyPrices: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
