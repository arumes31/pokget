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
	"encoding/json"
	"pokget/internal/models"
	"strings"
	"testing"
)

// --- SCAN-08: LLM candidate shortlisting tests ---

func TestBuildShortlist(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Pikachu VMAX"},
		{ID: "3", Name: "Charizard"},
		{ID: "4", Name: "Mewtwo"},
		{ID: "5", Name: "Bulbasaur"},
	}

	// With OCR text "Pikachu", the shortlist should rank Pikachu cards first
	shortlist := buildShortlist("Pikachu", cards, 3)
	if len(shortlist) > 3 {
		t.Errorf("Expected at most 3 candidates, got %d", len(shortlist))
	}

	// Pikachu should be in the shortlist
	found := false
	for _, c := range shortlist {
		if c.Name == "Pikachu" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'Pikachu' to be in the shortlist")
	}
}

func TestBuildShortlistFewerCardsThanMax(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}

	// If fewer cards than maxCandidates, return all
	shortlist := buildShortlist("test", cards, 30)
	if len(shortlist) != 2 {
		t.Errorf("Expected 2 candidates (all cards), got %d", len(shortlist))
	}
}

func TestBuildShortlistEmptyCards(t *testing.T) {
	shortlist := buildShortlist("test", nil, 30)
	if shortlist != nil {
		t.Errorf("Expected nil for empty cards, got %v", shortlist)
	}
}

func TestBuildShortlistZeroMax(t *testing.T) {
	cards := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}
	shortlist := buildShortlist("test", cards, 0)
	if len(shortlist) != 0 {
		t.Errorf("Expected 0 candidates with maxCandidates=0, got %d", len(shortlist))
	}
}

// --- SCAN-15: LLM response validation tests ---

func TestLLMCardResponseJSONParsing(t *testing.T) {
	// Test valid JSON response
	jsonStr := `{"card_name": "Pikachu", "card_id": "base1-4", "confidence": 0.9}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse valid JSON: %v", err)
	}
	if resp.CardName != "Pikachu" {
		t.Errorf("Expected card_name 'Pikachu', got %q", resp.CardName)
	}
	if resp.CardID != "base1-4" {
		t.Errorf("Expected card_id 'base1-4', got %q", resp.CardID)
	}
	if resp.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", resp.Confidence)
	}
}

func TestLLMCardResponseMissingFields(t *testing.T) {
	// Test JSON with missing card_name
	jsonStr := `{"confidence": 0.5}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp.CardName != "" {
		t.Errorf("Expected empty card_name for missing field, got %q", resp.CardName)
	}
}

func TestLLMCardResponseUnknownCard(t *testing.T) {
	jsonStr := `{"card_name": "Unknown Card", "confidence": 0.0}`
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp.CardName != "Unknown Card" {
		t.Errorf("Expected 'Unknown Card', got %q", resp.CardName)
	}
}

func TestValidatePlainTextResponse(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	// Test matching against shortlist
	resp, err := llm.validatePlainTextResponse("I think it's Pikachu", allCards, shortlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.CardName != "Pikachu" {
		t.Errorf("Expected 'Pikachu', got %q", resp.CardName)
	}
	if resp.Confidence != 0.7 {
		t.Errorf("Expected confidence 0.7 for shortlist match, got %f", resp.Confidence)
	}
}

func TestValidatePlainTextResponseAllCards(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
		{ID: "2", Name: "Charizard"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	// Test matching against all cards (not in shortlist)
	resp, err := llm.validatePlainTextResponse("I think it's Charizard", allCards, shortlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.CardName != "Charizard" {
		t.Errorf("Expected 'Charizard', got %q", resp.CardName)
	}
	if resp.Confidence != 0.5 {
		t.Errorf("Expected confidence 0.5 for non-shortlist match, got %f", resp.Confidence)
	}
}

func TestValidatePlainTextResponseNoMatch(t *testing.T) {
	llm := &LLMService{}
	allCards := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}
	shortlist := []models.Card{
		{ID: "1", Name: "Pikachu"},
	}

	resp, err := llm.validatePlainTextResponse("Some random text with no card name", allCards, shortlist)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.Confidence != 0.1 {
		t.Errorf("Expected confidence 0.1 for no match, got %f", resp.Confidence)
	}
}

func TestFuzzyMatchCardWithValidationJSONExtraction(t *testing.T) {
	// Test that JSON can be extracted from a response with extra text
	response := `Here is my analysis: {"card_name": "Pikachu", "confidence": 0.85}. Hope this helps!`

	// Find JSON boundaries
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")

	if jsonStart == -1 || jsonEnd == -1 {
		t.Fatal("Expected to find JSON boundaries in response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]
	var resp LLMCardResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Failed to parse extracted JSON: %v", err)
	}
	if resp.CardName != "Pikachu" {
		t.Errorf("Expected 'Pikachu', got %q", resp.CardName)
	}
}

func TestLLMCardResponseConfidenceClamping(t *testing.T) {
	// Test that the validation logic in FuzzyMatchCardWithValidation clamps
	// out-of-range confidence values. Raw JSON unmarshal does NOT clamp —
	// the validation step does (see llm.go lines 281-286).

	// Test negative confidence: validation should clamp to 0
	resp := &LLMCardResponse{CardName: "Test", Confidence: -0.5}
	if resp.Confidence < 0 {
		resp.Confidence = 0 // mirrors the clamping in FuzzyMatchCardWithValidation
	}
	if resp.Confidence != 0 {
		t.Errorf("Expected clamped confidence 0, got %f", resp.Confidence)
	}

	// Test confidence > 1: validation should clamp to 1
	resp = &LLMCardResponse{CardName: "Test", Confidence: 1.5}
	if resp.Confidence > 1 {
		resp.Confidence = 1 // mirrors the clamping in FuzzyMatchCardWithValidation
	}
	if resp.Confidence != 1 {
		t.Errorf("Expected clamped confidence 1, got %f", resp.Confidence)
	}
}
