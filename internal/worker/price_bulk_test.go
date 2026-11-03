package worker

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"pokget/internal/service"
)

func TestApplyBulkBatchUpdatesChangedPrices(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards AS c[\\s\\S]*price_variant").
		WithArgs("scryfall", "card-a", 1.25, 1.10, "foil", "ygoprodeck", "1234", 0.33, 0.02, "normal").
		WillReturnResult(sqlmock.NewResult(0, 2))
	// Cards whose quote did not move still get a fresh check stamp.
	mock.ExpectExec("UPDATE cards AS c[\\s\\S]*price_checked_at").
		WithArgs("scryfall", "card-a", "ygoprodeck", "1234").
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	result, err := worker.applyBulkBatch(context.Background(), []service.BulkPriceUpdate{
		{SourceID: "scryfall", SourceCardID: "card-a", USD: 1.25, EUR: 1.10, Variant: "foil"},
		{SourceID: "ygoprodeck", SourceCardID: "1234", USD: 0.33, EUR: 0.02, Variant: "normal"},
	})
	if err != nil {
		t.Fatalf("applyBulkBatch: %v", err)
	}
	if result.changed != 2 {
		t.Fatalf("changed = %d, want 2", result.changed)
	}
	if result.matched != 3 {
		t.Fatalf("matched = %d, want 3 (two changed + one verified)", result.matched)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyBulkSourceFlushesPartialBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE cards AS c[\\s\\S]*price_variant").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE cards AS c[\\s\\S]*price_checked_at").WillReturnResult(sqlmock.NewResult(0, 0))

	worker := &DataSyncWorker{db: db}
	err = worker.applyBulkSource(context.Background(), stubBulkSource{
		updates: []service.BulkPriceUpdate{{SourceID: "scryfall", SourceCardID: "card-a", USD: 1, EUR: 1}},
	})
	if err != nil {
		t.Fatalf("applyBulkSource: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyBulkSourcePropagatesErrors(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	worker := &DataSyncWorker{db: db}
	if err := worker.applyBulkSource(context.Background(), stubBulkSource{err: errors.New("download failed")}); err == nil {
		t.Fatal("expected the source error")
	}
}

// fuzzyIndex mirrors the index the bulk worker builds for one marketplace row.
func fuzzyIndex(set, number string, price float64) map[string]float64 {
	index := make(map[string]float64)
	for _, key := range priceMatchKeys(set, number) {
		index[key] = price
	}
	return index
}

func TestPriceMatchKeysCoversMarketplaceSpellings(t *testing.T) {
	// Each pair is (set code, number) as the catalog and the marketplace spell
	// it. Both sides must produce at least one common key.
	pairs := [][4]string{
		// leading zeros: catalog "sv02"/"001" vs TCGplayer "SV2"/"001/198"
		{"sv02", "001", "SV2", "001/198"},
		// UniVersus writes the set code with padding, TCGplayer without
		{"AOT01", "001", "AOT1", "1/246"},
		// Digimon appends the rarity to the number
		{"BT4", "BT4-001", "BT-04", "BT4-001 C"},
		// the set code is repeated inside the number
		{"hBP01", "hBP01-001", "HBP01", "001"},
		// UniVersus keeps the set code inside the number
		{"AOT1", "001", "AOT1", "AOT1 1/246"},
		// Weiss Schwarz abbreviations end with a slash
		{"KNK", "001", "KNK/", "001"},
		// Japanese PokÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â©mon numbers keep the print run
		{"SV3", "089", "SV3", "089/108"},
	}
	for _, pair := range pairs {
		catalog := priceMatchKeys(pair[0], pair[1])
		market := priceMatchKeys(pair[2], pair[3])
		matched := false
		for _, key := range catalog {
			for _, other := range market {
				if key == other {
					matched = true
				}
			}
		}
		if !matched {
			t.Fatalf("no shared key for catalog (%q,%q)=%v vs marketplace (%q,%q)=%v",
				pair[0], pair[1], catalog, pair[2], pair[3], market)
		}
	}
}

// Final Fantasy's catalog set codes ("Opus 1") cannot match TCGplayer's
// abbreviations ("OP1"), so those cards rely on the set-name index.
func TestPriceMatchKeysMatchOnSetNameWhenCodesDisagree(t *testing.T) {
	catalog := priceMatchKeys("Opus I", "1-001H")
	market := priceMatchKeys("Opus I", "1-001H")
	if len(catalog) == 0 || catalog[0] != market[0] {
		t.Fatalf("catalog %v vs marketplace %v", catalog, market)
	}
	if catalog[0] != "opusi|11h" {
		t.Fatalf("key = %q, want opusi|11h", catalog[0])
	}
}

func TestPriceMatchKeysDropsRarityAndPrintRun(t *testing.T) {
	keys := priceMatchKeys("BT27", "BT27-084 SR")
	want := map[string]bool{"bt27|bt2784": true, "bt27|84": true}
	for _, key := range keys {
		if !want[key] {
			t.Fatalf("unexpected key %q (all keys: %v)", key, keys)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing keys %v (got %v)", want, keys)
	}
}

func TestPriceMatchKeysKeepsAlphaNumericNumbers(t *testing.T) {
	// A number that is only letters is not a rarity suffix; it must survive.
	if keys := priceMatchKeys("DM", "P"); len(keys) != 1 || keys[0] != "dm|p" {
		t.Fatalf("keys = %v, want [dm|p]", keys)
	}
	if keys := priceMatchKeys("", "0001"); len(keys) != 1 || keys[0] != "|1" {
		t.Fatalf("zero padding must be dropped, got %v", keys)
	}
}

// Catalog PokÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â©mon rows carry TCGdex set codes ("me1") while TCGCSV groups use
// TCGplayer abbreviations ("MEG"); matching the normalized set name bridges that
// gap without a hand-maintained alias table.
func TestApplyFuzzyPricesMatchesSetNameWhenCodeMisses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-1", "me1", "Mega Evolution", "001", "Bulbasaur")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code").WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-1", 7.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byName := make(map[string]float64)
	for _, alias := range setNameAliases("ME01: Mega Evolution") {
		for key, price := range fuzzyIndex(alias, "001", 7.5) {
			byName[key] = price
		}
	}
	changed, err := worker.applyFuzzyPrices(context.Background(), "pokemon", nil, nil, byName)
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

func TestSetNameAliasesDropsTheSetCodePrefix(t *testing.T) {
	aliases := setNameAliases("SV01: Scarlet & Violet Base Set")
	if len(aliases) != 2 || aliases[0] != "SV01: Scarlet & Violet Base Set" || aliases[1] != "Scarlet & Violet Base Set" {
		t.Fatalf("aliases = %v", aliases)
	}
	if got := setNameAliases("  "); got != nil {
		t.Fatalf("blank name must produce no aliases, got %v", got)
	}
	if got := setNameAliases("Promos"); len(got) != 1 || got[0] != "Promos" {
		t.Fatalf("aliases = %v, want the name alone", got)
	}
}

func TestApplyFuzzyPricesPrefersTheSetCodeMatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-1", "OP-01", "Romance Dawn", "016", "Monkey D. Luffy")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code").WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-1", 11.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byCode := fuzzyIndex("OP01", "016", 11.0)
	byName := fuzzyIndex("Romance Dawn", "016", 99.0)
	if _, err := worker.applyFuzzyPrices(context.Background(), "one_piece", nil, byCode, byName); err != nil {
		t.Fatalf("applyFuzzyPrices: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the set-code match must win: %v", err)
	}
}

// The Japanese PokÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â©mon category prices only Japanese cards and runs before the
// language-agnostic pass, so a Japanese collector keeps the Japanese quote.
func TestApplyFuzzyPricesScopesLanguageCategories(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-jp", "SV3", "Ruler of the Black Flame", "089", "Nazonokusa")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code[\\s\\S]*ANY\\(\\$2::text\\[\\]\\)").
		WithArgs("pokemon", textArray{"{\"ja\"}"}, textArray{"{}"}).
		WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-jp", 12.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byCode := fuzzyIndex("SV3", "089/108", 12.5)
	changed, err := worker.applyFuzzyPrices(context.Background(), "pokemon", []string{"ja"}, byCode, nil)
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

// The English PokÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â©mon category must not price Japanese cards: Japanese sets reuse
// English set codes for different cards. The Japanese category fills them instead.
func TestApplyFuzzyPricesSkipsLanguagesWithTheirOwnCategory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-en", "sv3", "Obsidian Flames", "089", "Nazonokusa")
	// No language restriction, but Japanese is excluded.
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code[\\s\\S]*<> ALL\\(\\$3::text\\[\\]\\)").
		WithArgs("pokemon", emptyTextArray{}, textArray{"{\"ja\"}"}).
		WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-en", 3.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byCode := fuzzyIndex("SV3", "089/108", 3.0)
	changed, err := worker.applyFuzzyPricesExcluding(context.Background(), "pokemon", nil, []string{"ja"}, byCode, nil, nil, nil)
	if err != nil {
		t.Fatalf("applyFuzzyPricesExcluding: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// emptyTextArray matches the encoded empty Postgres array. A nil []string
// encodes as SQL NULL and `<> ALL(NULL)` would filter every row, which silently
// disabled the language-scoped (Japanese) fallback in production.
type emptyTextArray struct{}

func (emptyTextArray) Match(value driver.Value) bool {
	return textArrayValue(value) == "{}"
}

// textArray matches the encoded Postgres array exactly, so a test cannot pass
// with the wrong languages.
type textArray struct{ want string }

func (a textArray) Match(value driver.Value) bool {
	return textArrayValue(value) == a.want
}

func textArrayValue(value driver.Value) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	}
	return ""
}

func TestApplyFuzzyPricesSendsAnEmptyLanguageExclusion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "set_code", "set_name", "collector_number", "name"}).
		AddRow("card-jp", "SV3", "Ruler of the Black Flame", "001", "Bulbasaur")
	mock.ExpectQuery("SELECT id, COALESCE\\(set_code").
		WithArgs("pokemon", sqlmock.AnyArg(), emptyTextArray{}).
		WillReturnRows(rows)
	mock.ExpectExec("UPDATE cards AS c").
		WithArgs("card-jp", 0.18).
		WillReturnResult(sqlmock.NewResult(0, 1))

	worker := &DataSyncWorker{db: db}
	byCode := fuzzyIndex("SV3", "001/108", 0.18)
	if _, err := worker.applyFuzzyPrices(context.Background(), "pokemon", []string{"ja"}, byCode, nil); err != nil {
		t.Fatalf("applyFuzzyPrices: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the language filter must send an empty array, not NULL: %v", err)
	}
}

func TestOrderedFuzzyScopesPutsLanguageScopesFirst(t *testing.T) {
	index := map[fuzzyScope]map[string]float64{
		{game: "pokemon"}:                {},
		{game: "pokemon", languages: "ja"}: {},
		{game: "digimon"}:                {},
	}
	scopes := orderedFuzzyScopes(index)
	if scopes[0] != (fuzzyScope{game: "pokemon", languages: "ja"}) {
		t.Fatalf("scopes = %v, want the language scope first", scopes)
	}
	if scopes[1].game != "digimon" || scopes[2].game != "pokemon" {
		t.Fatalf("scopes = %v, want the remaining scopes sorted by game", scopes)
	}
}

func TestApplyBulkSourceSkipsWithoutSources(t *testing.T) {
	worker := &DataSyncWorker{}
	worker.syncBulkPrices(context.Background())
}

type stubBulkSource struct {
	updates []service.BulkPriceUpdate
	err     error
}

func (s stubBulkSource) Name() string { return "stub" }

func (s stubBulkSource) FetchPrices(_ context.Context, emit func(service.BulkPriceUpdate) error) error {
	if s.err != nil {
		return s.err
	}
	for _, update := range s.updates {
		if err := emit(update); err != nil {
			return err
		}
	}
	return nil
}
