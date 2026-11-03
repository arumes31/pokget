package worker

import "testing"

// TCGplayer sells the English release of a Japanese hololive set under the
// Japanese code plus a locale suffix; both sides must still meet.
func TestPriceSetVariantsDropTheLocaleSuffix(t *testing.T) {
	catalog := priceMatchKeys("hBP01", "hBP01-001")
	market := priceMatchKeys("hBP01e", "001")
	if len(catalog) == 0 || len(market) == 0 {
		t.Fatalf("catalog %v vs market %v", catalog, market)
	}
	shared := false
	for _, key := range catalog {
		for _, other := range market {
			if key == other {
				shared = true
			}
		}
	}
	if !shared {
		t.Fatalf("no shared key: catalog %v, market %v", catalog, market)
	}
}

func TestTrimLocaleSuffixLeavesRealCodesAlone(t *testing.T) {
	cases := []struct {
		code    string
		trimmed string
		ok      bool
	}{
		// canonicalPriceToken already drops the leading zero of the digit run.
		{"hbp01e", "hbp1", true},
		{"sd012e", "sd12", true},
		{"sv3a", "", false},
		{"bt4", "", false},
		{"me02.5", "", false}, // canonical "me25"
	}
	for _, tc := range cases {
		trimmed, ok := trimLocaleSuffix(canonicalPriceToken(tc.code))
		if ok != tc.ok || trimmed != tc.trimmed {
			t.Fatalf("trimLocaleSuffix(%q) = (%q, %v), want (%q, %v)", tc.code, trimmed, ok, tc.trimmed, tc.ok)
		}
	}
}

// Old marketplaces list products without a collector number, so the match falls
// back to the card name inside the set.
func TestPriceNameKeyIgnoresFormatting(t *testing.T) {
	if got := priceNameKey("Dungeon Treasure", "Lieutenant Horatio Laine"); got != priceNameKey("dungeon-treasure", "Lieutenant  Horatio Laine") {
		t.Fatalf("name keys differ: %q", got)
	}
	if priceNameKey("Dungeon Treasure", "Hogger") == priceNameKey("Dungeon Treasure", "Vancleef") {
		t.Fatal("different names must not collide")
	}
}

// Weiss Schwarz numbers carry the set code with a slash and mark foils with a
// letter suffix, so the plain printing must still match.
func TestPriceMatchKeysHandlesWeissNumbers(t *testing.T) {
	catalog := priceMatchKeys("NIK/S135", "NIK/S135-E063S")
	market := priceMatchKeys("NIK/S135", "NIK/S135-E063")
	shared := false
	for _, key := range catalog {
		for _, other := range market {
			if key == other {
				shared = true
			}
		}
	}
	if !shared {
		t.Fatalf("no shared key: catalog %v, market %v", catalog, market)
	}
}

// TCGplayer abbreviates the Weiss set as "NIK/" while both sides spell the number
// with the full set code, so the number alone identifies the card.
func TestPriceNumberOnlyKeyMatchesAcrossSetAbbreviations(t *testing.T) {
	catalog := priceNumberOnlyKey("NIK/S135-E063S")
	market := priceNumberOnlyKey("NIK/S135-E063")
	if catalog == "" || catalog != market {
		t.Fatalf("catalog %q vs market %q", catalog, market)
	}
	for _, plain := range []string{"", "089", "1", "OP01"} {
		if key := priceNumberOnlyKey(plain); key != "" {
			t.Fatalf("priceNumberOnlyKey(%q) = %q, want no key for a plain number", plain, key)
		}
	}
}

func TestTrimVariantLetters(t *testing.T) {
	// canonicalPriceToken drops the leading zero of each digit run, so "E063"
	// becomes "e63" on both sides and still matches.
	cases := map[string]string{
		"NIK/S135-E063SP":               "niks135e63",
		"NIK/S135-E063":                 "niks135e63",
		"1-001H":                        "11",
		"006-aaj_kegon_ghost_crabs-b-f": "6aajkegonghostcrabsbf",
		"089/108":                       "89108",
	}
	for input, want := range cases {
		if got := trimVariantLetters(input); got != want {
			t.Fatalf("trimVariantLetters(%q) = %q, want %q", input, got, want)
		}
	}
}

// Names in Japanese or Chinese script keep no ASCII letter after canonicalization;
// they must not collapse into a single key that matches every such card.
func TestPriceNameKeyRejectsUnusableNames(t *testing.T) {
	for _, name := range []string{"", "  ", "ナゾノクサ", "妙蛙種子", "A"} {
		if key := priceNameKey("Dungeon Treasure", name); key != "" {
			t.Fatalf("priceNameKey(%q) = %q, want an empty key", name, key)
		}
	}
}
