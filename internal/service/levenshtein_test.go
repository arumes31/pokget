// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"testing"
)

// --- SCAN-13: Levenshtein rune fix verification tests ---

func TestLevenshteinBasic(t *testing.T) {
	tests := []struct {
		s1, s2   string
		expected int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"saturday", "sunday", 3},
		{"a", "b", 1},
		{"ab", "ab", 0},
	}

	for _, tt := range tests {
		got := levenshtein(tt.s1, tt.s2)
		if got != tt.expected {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
		}
	}
}

func TestLevenshteinCaseInsensitive(t *testing.T) {
	// The levenshtein function converts to lowercase internally
	dist1 := levenshtein("Pikachu", "pikachu")
	if dist1 != 0 {
		t.Errorf("Expected distance 0 for case-insensitive match, got %d", dist1)
	}

	dist2 := levenshtein("CHARIZARD", "charizard")
	if dist2 != 0 {
		t.Errorf("Expected distance 0 for case-insensitive match, got %d", dist2)
	}
}

func TestLevenshteinCJK(t *testing.T) {
	// SCAN-13: Verify that the function works correctly with CJK characters
	// by operating on runes instead of bytes

	// Japanese katakana: ピカチュウ vs ピカチュア (last char differs)
	dist := levenshtein("ピカチュウ", "ピカチュア")
	if dist != 1 {
		t.Errorf("Expected distance 1 for CJK single-char difference, got %d", dist)
	}

	// Same CJK string
	dist = levenshtein("ピカチュウ", "ピカチュウ")
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical CJK strings, got %d", dist)
	}

	// Empty vs CJK
	dist = levenshtein("", "漢字")
	if dist != 2 {
		t.Errorf("Expected distance 2 for empty vs 2-rune CJK, got %d", dist)
	}

	// Chinese: 卡比兽 vs 卡比 (missing last char)
	dist = levenshtein("卡比兽", "卡比")
	if dist != 1 {
		t.Errorf("Expected distance 1 for CJK missing char, got %d", dist)
	}

	// Korean: 피카츄 vs 피카츄 (identical)
	dist = levenshtein("피카츄", "피카츄")
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical Korean, got %d", dist)
	}
}

func TestLevenshteinMixedScript(t *testing.T) {
	// Mixed Latin + CJK
	dist := levenshtein("Pikachu VMAX", "Pikachu VMAX")
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical mixed strings, got %d", dist)
	}

	// Small typo in mixed string
	dist = levenshtein("Pikachu VMAX", "Pikachu VMX")
	if dist != 1 {
		t.Errorf("Expected distance 1 for single-char difference, got %d", dist)
	}
}

func TestLevenshteinSymmetry(t *testing.T) {
	// Distance should be symmetric: d(a,b) == d(b,a)
	tests := []struct {
		s1, s2 string
	}{
		{"kitten", "sitting"},
		{"Pikachu", "Charizard"},
		{"ピカチュウ", "ピカチュア"},
		{"abc", "def"},
	}

	for _, tt := range tests {
		d1 := levenshtein(tt.s1, tt.s2)
		d2 := levenshtein(tt.s2, tt.s1)
		if d1 != d2 {
			t.Errorf("levenshtein is not symmetric: d(%q,%q)=%d, d(%q,%q)=%d",
				tt.s1, tt.s2, d1, tt.s2, tt.s1, d2)
		}
	}
}

func TestLevenshteinTriangleInequality(t *testing.T) {
	// d(a,c) <= d(a,b) + d(b,c)
	a, b, c := "kitten", "sitting", "kitchen"
	dab := levenshtein(a, b)
	dbc := levenshtein(b, c)
	dac := levenshtein(a, c)
	if dac > dab+dbc {
		t.Errorf("Triangle inequality violated: d(%q,%q)=%d > d(%q,%q)=%d + d(%q,%q)=%d = %d",
			a, c, dac, a, b, dab, b, c, dbc, dab+dbc)
	}
}

// --- NEW: Comprehensive Levenshtein tests ---

// TestLevenshteinASCIITableDriven verifies ASCII string distance calculations
// using table-driven tests for common Pokémon card name typos.
func TestLevenshteinASCIITableDriven(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		// Pokémon card name typos
		{"Pikachu vs Pikachuu", "Pikachu", "Pikachuu", 1},
		{"Charizard vs Charzard", "Charizard", "Charzard", 1},
		{"Mewtwo vs Mewto", "Mewtwo", "Mewto", 1},
		// Bulbasaur vs Bulbasor: two substitutions (a→o at pos 7, u→o at pos 8)
		{"Bulbasaur vs Bulbasor", "Bulbasaur", "Bulbasor", 2},
		// Identical strings
		{"Identical", "Pikachu", "Pikachu", 0},
		// Completely different strings
		{"Completely different", "abc", "xyz", 3},
		// Single character
		{"Single char diff", "a", "b", 1},
		// Insertion
		{"Insertion at end", "abc", "abcd", 1},
		// Deletion
		{"Deletion at end", "abcd", "abc", 1},
		// Substitution
		{"Substitution", "abc", "axc", 1},
		// Transposition (Levenshtein doesn't handle this specially)
		{"Transposition", "ab", "ba", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s1, tt.s2)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
			}
		})
	}
}

// TestLevenshteinCJKTableDriven verifies CJK string distance calculations
// are rune-based, not byte-based.
func TestLevenshteinCJKTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		// Japanese: ピカチュウ vs ピカチュウV — appending "V" suffix
		{"Japanese with V suffix", "ピカチュウ", "ピカチュウV", 1},
		// Japanese: single character substitution
		{"Japanese single char diff", "リザードン", "リザードア", 1},
		// Chinese: single character deletion
		{"Chinese deletion", "皮卡丘", "皮卡", 1},
		// Korean: identical strings
		{"Korean identical", "피카츄", "피카츄", 0},
		// Korean: single character difference
		{"Korean single char diff", "피카츄", "피카츄V", 1},
		// Empty vs CJK — ピカチュウ has 5 runes
		{"Empty vs Japanese", "", "ピカチュウ", 5},
		// CJK vs empty
		{"Japanese vs empty", "ピカチュウ", "", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s1, tt.s2)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
			}
		})
	}
}

// TestLevenshteinMixedScriptTableDriven verifies mixed Latin+CJK
// comparisons work correctly with rune-based algorithm.
func TestLevenshteinMixedScriptTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		// Mixed script: Pikachu VMAX vs ピカチュウ VMAX
		// "Pikachu" (7 runes) vs "ピカチュウ" (5 runes) — completely different runes
		// " VMAX" (5 runes) is shared
		{"Mixed Latin and CJK", "Pikachu VMAX", "ピカチュウ VMAX", 7},
		// Same mixed string
		{"Identical mixed", "Pikachu VMAX", "Pikachu VMAX", 0},
		// CJK with Latin suffix
		{"CJK with Latin suffix", "ピカチュウV", "ピカチュウVMAX", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s1, tt.s2)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
			}
		})
	}
}

// TestLevenshteinEmptyStrings verifies edge case handling with empty strings.
func TestLevenshteinEmptyStrings(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		{"Both empty", "", "", 0},
		{"First empty", "", "abc", 3},
		{"Second empty", "abc", "", 3},
		// ピカチュウ has 5 runes (ピ,カ,チ,ュ,ウ)
		{"First empty CJK", "", "ピカチュウ", 5},
		{"Second empty CJK", "ピカチュウ", "", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s1, tt.s2)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
			}
		})
	}
}

// TestLevenshteinIdenticalStrings verifies distance 0 for identical strings.
func TestLevenshteinIdenticalStrings(t *testing.T) {
	tests := []struct {
		name string
		s    string
	}{
		{"ASCII", "Pikachu"},
		{"Japanese", "ピカチュウ"},
		{"Chinese", "皮卡丘"},
		{"Korean", "피카츄"},
		{"Mixed", "Pikachu VMAX"},
		{"Empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s, tt.s)
			if got != 0 {
				t.Errorf("levenshtein(%q, %q) = %d, want 0", tt.s, tt.s, got)
			}
		})
	}
}

// TestLevenshteinCompletelyDifferentStrings verifies maximum distance
// for completely different strings.
func TestLevenshteinCompletelyDifferentStrings(t *testing.T) {
	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		{"abc vs xyz", "abc", "xyz", 3},
		{"a vs z", "a", "z", 1},
		{"Pikachu vs Charizard", "Pikachu", "Charizard", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := levenshtein(tt.s1, tt.s2)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.expected)
			}
		})
	}
}

// TestLevenshteinCJKNotByteBased verifies that the Levenshtein function
// operates on runes, not bytes. This is critical for CJK characters
// which are multi-byte in UTF-8.
func TestLevenshteinCJKNotByteBased(t *testing.T) {
	// ピカチュウ is 5 runes but 15 bytes in UTF-8
	// If the function operated on bytes, distance would be much larger
	s1 := "ピカチュウ"
	s2 := "ピカチュア" // Only last rune differs

	dist := levenshtein(s1, s2)
	if dist != 1 {
		t.Errorf("Expected rune-based distance 1 for single CJK char difference, got %d (byte-based would be larger)", dist)
	}

	// Verify: deleting one CJK rune should give distance 1
	dist = levenshtein("カビ兽", "カビ")
	if dist != 1 {
		t.Errorf("Expected rune-based distance 1 for CJK deletion, got %d", dist)
	}
}

// TestLevenshteinLongStrings verifies that the space-optimized algorithm
// works correctly for longer strings.
func TestLevenshteinLongStrings(t *testing.T) {
	// Long ASCII strings
	s1 := "Pikachu VMAX Rainbow Secret Rare"
	s2 := "Pikachu VMAX Rainbow Secret Rar" // Missing last char
	dist := levenshtein(s1, s2)
	if dist != 1 {
		t.Errorf("Expected distance 1 for long string deletion, got %d", dist)
	}

	// Long CJK string
	s1 = "ピカチュウVMAX レインボーレア"
	s2 = "ピカチュウVMAX レインボーレア" // Identical
	dist = levenshtein(s1, s2)
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical long CJK strings, got %d", dist)
	}
}

// TestLevenshteinSymmetryCJK verifies symmetry for CJK strings.
func TestLevenshteinSymmetryCJK(t *testing.T) {
	tests := []struct {
		s1, s2 string
	}{
		{"ピカチュウ", "リザードン"},
		{"皮卡丘", "皮卡"},
		{"피카츄", "리자몽"},
		{"Pikachu", "ピカチュウ"},
	}

	for _, tt := range tests {
		d1 := levenshtein(tt.s1, tt.s2)
		d2 := levenshtein(tt.s2, tt.s1)
		if d1 != d2 {
			t.Errorf("levenshtein not symmetric for CJK: d(%q,%q)=%d, d(%q,%q)=%d",
				tt.s1, tt.s2, d1, tt.s2, tt.s1, d2)
		}
	}
}
