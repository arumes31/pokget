package source

import "testing"

func TestFFTCGLanguagesKeepsOnlyRenderedLanguages(t *testing.T) {
	got := fftcgLanguages([]string{"en", "de", "ja", "zh-cn", "it", "de"})
	want := []string{"en", "de", "it"}
	if len(got) != len(want) {
		t.Fatalf("fftcgLanguages = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("fftcgLanguages = %v, want %v", got, want)
		}
	}
	if got := fftcgLanguages([]string{"ja"}); len(got) != 1 || got[0] != "en" {
		t.Fatalf("unsupported-only list must fall back to en, got %v", got)
	}
}
