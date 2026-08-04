package catalog

import (
	"strings"
	"testing"
)

func TestCanonicalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		parts   []string
		wantErr bool
	}{
		{name: "valid card id", kind: "card", parts: []string{"tcgdex", "base1-4", "en"}},
		{name: "kind may contain underscore", kind: "card_image", parts: []string{"source", "one"}},
		{name: "missing kind", kind: "", parts: []string{"source"}, wantErr: true},
		{name: "uppercase kind", kind: "Card", parts: []string{"source"}, wantErr: true},
		{name: "missing parts", kind: "card", wantErr: true},
		{name: "blank part", kind: "card", parts: []string{"source", " "}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := CanonicalID(test.kind, test.parts...)
			if test.wantErr {
				if err == nil {
					t.Fatalf("CanonicalID() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalID() error = %v", err)
			}
			if !strings.HasPrefix(got, test.kind+"_") {
				t.Fatalf("CanonicalID() = %q, want prefix %q", got, test.kind+"_")
			}
			if len(got) != len(test.kind)+1+64 {
				t.Fatalf("CanonicalID() length = %d, want %d", len(got), len(test.kind)+1+64)
			}
		})
	}
}

func TestCanonicalID_IsDeterministicAndNamespaced(t *testing.T) {
	t.Parallel()

	first, err := CardID("tcgdex", "base1-4", "en")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CardID("tcgdex", "base1-4", "en")
	if err != nil {
		t.Fatal(err)
	}
	otherSource, err := CardID("other", "base1-4", "en")
	if err != nil {
		t.Fatal(err)
	}
	otherLanguage, err := CardID("tcgdex", "base1-4", "de")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("same input produced %q and %q", first, second)
	}
	if first == otherSource {
		t.Fatal("different sources produced the same id")
	}
	if first == otherLanguage {
		t.Fatal("different languages produced the same id")
	}
}

func TestCanonicalID_PreservesPartBoundaries(t *testing.T) {
	t.Parallel()

	first, err := CanonicalID("card", "ab", "c")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalID("card", "a", "bc")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different part boundaries produced the same id")
	}
}
