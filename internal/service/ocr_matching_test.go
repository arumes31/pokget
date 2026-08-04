package service

import "testing"

func TestFuzzySubstringMatchShortLatinNameRequiresWholeToken(t *testing.T) {
	t.Parallel()

	if fuzzySubstringMatch("TRAINER", "N") {
		t.Fatal("embedded one-letter name matched OCR text")
	}
	if !fuzzySubstringMatch("TRAINER N 123", "N") {
		t.Fatal("whole-token one-letter name did not match OCR text")
	}
	if fuzzySubstringMatch("STONE", "ONE") {
		t.Fatal("embedded three-letter name matched OCR text")
	}
	if !fuzzySubstringMatch("ONE / 001", "ONE") {
		t.Fatal("whole-token three-letter name did not match OCR text")
	}
}
