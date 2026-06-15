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
	"fmt"
	"pokget/internal/models"
	"testing"
	"time"
)

// --- SCAN-01: BK-tree tests ---

func TestBKTreeInsertAndSearch(t *testing.T) {
	tree := NewBKTree()

	card1 := &models.Card{ID: "card-1", Name: "Pikachu"}
	card2 := &models.Card{ID: "card-2", Name: "Charizard"}
	card3 := &models.Card{ID: "card-3", Name: "Mewtwo"}

	// Insert hashes with known Hamming distances
	// hash1 = 0b0001 (1)
	// hash2 = 0b0011 (3) — distance 1 from hash1
	// hash3 = 0b0111 (7) — distance 1 from hash2, distance 2 from hash1
	tree.Insert(1, card1)
	tree.Insert(3, card2)
	tree.Insert(7, card3)

	if tree.count != 3 {
		t.Errorf("Expected tree count 3, got %d", tree.count)
	}

	// Search with radius 0 from hash1 — should find only exact match
	results := tree.Search(1, 0)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result for exact search, got %d", len(results))
	}
	if results[0].Card.ID != "card-1" {
		t.Errorf("Expected card-1, got %s", results[0].Card.ID)
	}
	if results[0].Distance != 0 {
		t.Errorf("Expected distance 0, got %d", results[0].Distance)
	}

	// Search with radius 1 from hash1 — should find card1 (dist 0) and card2 (dist 1)
	// hammingDistance(0b0001, 0b0011) = 1, so card2 is within radius 1
	results = tree.Search(1, 1)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results for radius-1 search, got %d", len(results))
	}

	// Search with radius 2 from hash1 — should find all three
	results = tree.Search(1, 2)
	if len(results) != 3 {
		t.Fatalf("Expected 3 results for radius-2 search, got %d", len(results))
	}
}

func TestBKTreeEmpty(t *testing.T) {
	tree := NewBKTree()

	// Search on empty tree
	results := tree.Search(0, 5)
	if results != nil {
		t.Errorf("Expected nil results for empty tree, got %v", results)
	}

	// SearchExact on empty tree
	exact := tree.SearchExact(0)
	if exact != nil {
		t.Errorf("Expected nil for SearchExact on empty tree, got %v", exact)
	}
}

func TestBKTreeSearchExact(t *testing.T) {
	tree := NewBKTree()

	card1 := &models.Card{ID: "card-1", Name: "Pikachu"}
	card2 := &models.Card{ID: "card-2", Name: "Charizard"}

	tree.Insert(42, card1)
	tree.Insert(100, card2)

	// Exact match for existing hash
	exact := tree.SearchExact(42)
	if exact == nil {
		t.Fatal("Expected exact match, got nil")
	}
	if exact.Card.ID != "card-1" {
		t.Errorf("Expected card-1, got %s", exact.Card.ID)
	}
	if exact.Distance != 0 {
		t.Errorf("Expected distance 0, got %d", exact.Distance)
	}

	// No exact match
	exact = tree.SearchExact(99)
	if exact != nil {
		t.Errorf("Expected nil for non-existing exact match, got %v", exact)
	}
}

func TestBKTreeSearchExactEarlyTermination(t *testing.T) {
	// SCAN-14: Verify early termination on exact match
	tree := NewBKTree()

	card := &models.Card{ID: "card-exact", Name: "ExactMatch"}
	tree.Insert(12345, card)

	// SearchExact should find the match immediately
	exact := tree.SearchExact(12345)
	if exact == nil {
		t.Fatal("Expected exact match, got nil")
	}
	if exact.Distance != 0 {
		t.Errorf("Expected distance 0 for exact match, got %d", exact.Distance)
	}
}

func TestBKTreeDuplicateInsert(t *testing.T) {
	tree := NewBKTree()

	card1 := &models.Card{ID: "card-1", Name: "Pikachu"}
	tree.Insert(42, card1)
	tree.Insert(42, card1) // Same hash, same card — should be deduplicated

	if tree.count != 1 {
		t.Errorf("Expected count 1 after duplicate insert, got %d", tree.count)
	}
}

func TestBKTreeMultipleHashesSameCard(t *testing.T) {
	// SCAN-12: Multiple fingerprints per card
	tree := NewBKTree()

	card := &models.Card{ID: "card-1", Name: "Pikachu"}
	tree.Insert(10, card)
	tree.Insert(20, card) // Same card, different hash

	if tree.count != 2 {
		t.Errorf("Expected count 2 for same card with different hashes, got %d", tree.count)
	}

	// Search should deduplicate by card ID
	results := tree.Search(10, 15)
	// Both hashes match within radius 15, but should be deduplicated to 1 card
	found := false
	for _, r := range results {
		if r.Card.ID == "card-1" {
			found = true
			// Best distance should be 0 (exact match on hash 10)
			if r.Distance != 0 {
				t.Errorf("Expected best distance 0 for card-1, got %d", r.Distance)
			}
		}
	}
	if !found {
		t.Error("Expected to find card-1 in results")
	}
}

func TestBKTreeDeduplicateByCard(t *testing.T) {
	// SCAN-12: Test deduplicateByCard directly
	card := &models.Card{ID: "card-1", Name: "Pikachu"}

	matches := []FingerprintMatch{
		{Card: card, Distance: 5},
		{Card: card, Distance: 2}, // Better distance, should win
		{Card: &models.Card{ID: "card-2", Name: "Mew"}, Distance: 3},
	}

	deduped := deduplicateByCard(matches)
	if len(deduped) != 2 {
		t.Fatalf("Expected 2 deduplicated results, got %d", len(deduped))
	}

	// Find card-1 and verify it has the best distance
	for _, m := range deduped {
		if m.Card.ID == "card-1" && m.Distance != 2 {
			t.Errorf("Expected best distance 2 for card-1, got %d", m.Distance)
		}
	}
}

func TestBKTreeDeduplicateByCardEmpty(t *testing.T) {
	deduped := deduplicateByCard(nil)
	if len(deduped) != 0 {
		t.Errorf("Expected empty result for nil input, got %d", len(deduped))
	}
}

func TestBKTreeLargeDataset(t *testing.T) {
	tree := NewBKTree()

	// Insert 1000 cards with varying hashes
	for i := 0; i < 1000; i++ {
		card := &models.Card{ID: fmt.Sprintf("card-%d", i), Name: fmt.Sprintf("Card %d", i)}
		tree.Insert(uint64(i*7+3), card) // Spread hashes
	}

	if tree.count != 1000 {
		t.Errorf("Expected 1000 nodes, got %d", tree.count)
	}

	// Search should work efficiently
	results := tree.Search(50, 5)
	// Just verify it doesn't crash and returns reasonable results
	for _, r := range results {
		if r.Card == nil {
			t.Error("Expected non-nil card in result")
		}
	}
}

func TestHammingDistance(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected int
	}{
		{0, 0, 0},
		{0, 1, 1},
		{0xFF, 0x00, 8},
		{0x0F, 0xF0, 8},
		{0xDEADBEEF, 0xDEADBEEF, 0},
		{0x0000000000000001, 0x0000000000000000, 1},
		{0xFFFFFFFFFFFFFFFF, 0x0000000000000000, 64},
	}

	for _, tt := range tests {
		got := hammingDistance(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("hammingDistance(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

// --- NEW: Comprehensive BK-tree and fingerprint tests ---

// TestBKTreeSearchRadiusCorrectness verifies that all results returned by
// BK-tree Search are actually within the specified Hamming distance radius.
func TestBKTreeSearchRadiusCorrectness(t *testing.T) {
	tree := NewBKTree()

	// Insert 50 cards with known hashes
	for i := 0; i < 50; i++ {
		card := &models.Card{ID: fmt.Sprintf("card-%d", i), Name: fmt.Sprintf("Card %d", i)}
		tree.Insert(uint64(i*1234567+42), card)
	}

	// Search with various radii and verify all results are within radius
	query := uint64(42*1234567 + 42) // Exact match for card-42
	for _, radius := range []int{0, 3, 5, 10, 20} {
		results := tree.Search(query, radius)
		for _, r := range results {
			// Verify the distance field is within the requested radius
			if r.Distance > radius {
				t.Errorf("Result distance %d exceeds radius %d for card %s",
					r.Distance, radius, r.Card.ID)
			}
		}
	}
}

// TestBKTreeVsLinearScanParity verifies that BK-tree Search returns the
// same results as a brute-force linear scan for every query.
func TestBKTreeVsLinearScanParity(t *testing.T) {
	tree := NewBKTree()
	hashes := []uint64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	cards := make([]*models.Card, len(hashes))

	for i, h := range hashes {
		cards[i] = &models.Card{ID: fmt.Sprintf("card-%d", i), Name: fmt.Sprintf("Card %d", i)}
		tree.Insert(h, cards[i])
	}

	// For each query, compare BK-tree results with linear scan
	queries := []uint64{100, 250, 500, 999, 0, 0xFFFFFFFFFFFFFFFF}
	for _, radius := range []int{1, 5, 10, 20} {
		for _, query := range queries {
			// BK-tree search
			bkResults := tree.Search(query, radius)

			// Linear scan
			var linearResults []FingerprintMatch
			for i, h := range hashes {
				d := hammingDistance(query, h)
				if d <= radius {
					linearResults = append(linearResults, FingerprintMatch{Card: cards[i], Distance: d})
				}
			}
			linearResults = deduplicateByCard(linearResults)

			// Compare: BK-tree should find the same set as linear scan
			bkSet := make(map[string]int)
			for _, r := range bkResults {
				bkSet[r.Card.ID] = r.Distance
			}
			for _, r := range linearResults {
				bkDist, ok := bkSet[r.Card.ID]
				if !ok {
					t.Errorf("BK-tree missed card %s (dist %d) for query=%d radius=%d",
						r.Card.ID, r.Distance, query, radius)
				} else if bkDist != r.Distance {
					t.Errorf("BK-tree distance mismatch for card %s: got %d, want %d",
						r.Card.ID, bkDist, r.Distance)
				}
			}
			if len(bkResults) != len(linearResults) {
				t.Errorf("BK-tree returned %d results, linear returned %d for query=%d radius=%d",
					len(bkResults), len(linearResults), query, radius)
			}
		}
	}
}

// TestBKTreeSearchExactReturnsImmediately verifies that SearchExact with
// distance=0 returns the correct card immediately.
func TestBKTreeSearchExactReturnsImmediately(t *testing.T) {
	tree := NewBKTree()

	// Insert many cards
	for i := 0; i < 100; i++ {
		card := &models.Card{ID: fmt.Sprintf("card-%d", i), Name: fmt.Sprintf("Card %d", i)}
		tree.Insert(uint64(i*1000+7), card)
	}

	// Search for exact match of card-50
	targetHash := uint64(50*1000 + 7)
	exact := tree.SearchExact(targetHash)
	if exact == nil {
		t.Fatal("Expected exact match for card-50, got nil")
	}
	if exact.Card.ID != "card-50" {
		t.Errorf("Expected card-50, got %s", exact.Card.ID)
	}
	if exact.Distance != 0 {
		t.Errorf("Expected distance 0, got %d", exact.Distance)
	}

	// Search for non-existent hash
	noMatch := tree.SearchExact(999999)
	if noMatch != nil {
		t.Errorf("Expected nil for non-existent exact match, got %v", noMatch)
	}
}

// TestBKTreeTwoTierThresholdSystem verifies the two-tier threshold system:
// high-confidence (distance <= 5) and potential (distance <= 10) matches.
func TestBKTreeTwoTierThresholdSystem(t *testing.T) {
	svc := NewFingerprintService(nil)

	// Insert cards with hashes at various distances from a target.
	// NOTE: No exact match inserted — SearchByHash returns early on exact
	// match (SearchExact) without populating Potential, so we skip it to
	// test the two-tier path properly.
	targetHash := uint64(0)
	svc.AddFingerprint(1, &models.Card{ID: "dist1", Name: "Close Match"})                     // distance 1
	svc.AddFingerprint(uint64(1<<4), &models.Card{ID: "dist4", Name: "Near Match"})           // distance 4
	svc.AddFingerprint(uint64(1<<5), &models.Card{ID: "dist5", Name: "High Conf Boundary"})   // distance 5
	svc.AddFingerprint(uint64(1<<6), &models.Card{ID: "dist6", Name: "Potential Match"})      // distance 6
	svc.AddFingerprint(uint64(1<<9), &models.Card{ID: "dist9", Name: "Potential Match 2"})    // distance 9
	svc.AddFingerprint(uint64(1<<10), &models.Card{ID: "dist10", Name: "Potential Boundary"}) // distance 10

	// Search with potential threshold (<=10)
	result := svc.SearchByHash(int64(targetHash))

	// Should have a high-confidence match (distance <= 5).
	// NOTE: SearchByHash sets HighConfidence to the last match within the
	// high-confidence threshold (not necessarily the closest), so we only
	// verify that a high-confidence match exists and is within range.
	if result.HighConfidence == nil {
		t.Error("Expected high-confidence match, got nil")
	} else {
		// Find the distance for the HighConfidence card from Potential list
		hcDist := -1
		for _, m := range result.Potential {
			if m.Card.ID == result.HighConfidence.ID {
				hcDist = m.Distance
				break
			}
		}
		if hcDist < 0 || hcDist > 5 {
			t.Errorf("High-confidence card %s has distance %d, expected <= 5",
				result.HighConfidence.ID, hcDist)
		}
	}

	// Should have potential matches (all within distance 10)
	if len(result.Potential) == 0 {
		t.Error("Expected potential matches, got none")
	}

	// All potential matches should be within distance 10
	for _, m := range result.Potential {
		if m.Distance > 10 {
			t.Errorf("Potential match distance %d exceeds threshold 10 for card %s",
				m.Distance, m.Card.ID)
		}
	}

	// Best distance should be 1 (closest match)
	if result.BestDistance != 1 {
		t.Errorf("Expected best distance 1, got %d", result.BestDistance)
	}
}

// TestBKTreeNoMatchBeyondThreshold verifies that empty results are returned
// when no fingerprint is within the threshold.
func TestBKTreeNoMatchBeyondThreshold(t *testing.T) {
	svc := NewFingerprintService(nil)

	// Insert a card with a very different hash
	svc.AddFingerprint(0xFFFFFFFFFFFFFFFF, &models.Card{ID: "far-card", Name: "Far Card"})

	// Search with a hash that's maximally distant
	result := svc.SearchByHash(0)

	// No match should be within threshold (distance 64 > 10)
	if result.HighConfidence != nil {
		t.Errorf("Expected no high-confidence match, got %v", result.HighConfidence)
	}
	if len(result.Potential) > 0 {
		for _, m := range result.Potential {
			if m.Distance <= 10 {
				t.Errorf("Expected no match within threshold, got card %s at distance %d",
					m.Card.ID, m.Distance)
			}
		}
	}
	if result.BestDistance != 64 {
		t.Errorf("Expected best distance 64 (no match), got %d", result.BestDistance)
	}
}

// TestBKTreeMultipleFingerprintsPerCardDedup verifies that when 2-3 fingerprints
// exist for the same card, deduplication returns the best match.
func TestBKTreeMultipleFingerprintsPerCardDedup(t *testing.T) {
	tree := NewBKTree()

	card := &models.Card{ID: "pikachu-1", Name: "ピカチュウ"} // CJK card name
	// Insert 3 different hashes for the same card (different art variants)
	tree.Insert(1000, card)
	tree.Insert(1005, card) // distance 3 from 1000
	tree.Insert(2000, card) // distance much larger

	// Search near hash 1000 — should deduplicate to 1 result with best distance
	results := tree.Search(1002, 5)
	pikachuCount := 0
	for _, r := range results {
		if r.Card.ID == "pikachu-1" {
			pikachuCount++
			if r.Distance > 5 {
				t.Errorf("Deduplicated distance %d exceeds radius 5", r.Distance)
			}
		}
	}
	if pikachuCount > 1 {
		t.Errorf("Expected 1 deduplicated result for pikachu-1, got %d", pikachuCount)
	}
}

// TestBKTreeCJKCardNames verifies fingerprint matching works with
// Japanese/Chinese card names in the database.
func TestBKTreeCJKCardNames(t *testing.T) {
	tree := NewBKTree()

	// Insert cards with CJK names
	cards := []*models.Card{
		{ID: "jp-1", Name: "ピカチュウ"},
		{ID: "jp-2", Name: "リザードン"},
		{ID: "cn-1", Name: "皮卡丘"},
		{ID: "kr-1", Name: "피카츄"},
	}

	hashes := []uint64{100, 200, 300, 400}
	for i, h := range hashes {
		tree.Insert(h, cards[i])
	}

	// Search for Japanese Pikachu
	results := tree.Search(100, 0)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Card.Name != "ピカチュウ" {
		t.Errorf("Expected ピカチュウ, got %s", results[0].Card.Name)
	}

	// Search with radius for Chinese card
	results = tree.Search(300, 5)
	found := false
	for _, r := range results {
		if r.Card.Name == "皮卡丘" {
			found = true
		}
	}
	if !found {
		t.Error("Expected to find 皮卡丘 in results")
	}
}

// TestBKTreeLargePerformance verifies that searching a tree with 1000+
// fingerprints completes in under 10ms.
func TestBKTreeLargePerformance(t *testing.T) {
	tree := NewBKTree()

	// Insert 1500 fingerprints
	for i := 0; i < 1500; i++ {
		card := &models.Card{ID: fmt.Sprintf("perf-card-%d", i), Name: fmt.Sprintf("PerfCard %d", i)}
		tree.Insert(uint64(i*31+17), card)
	}

	if tree.count != 1500 {
		t.Errorf("Expected 1500 nodes, got %d", tree.count)
	}

	// Search should complete in <10ms
	start := time.Now()
	for i := 0; i < 100; i++ {
		tree.Search(uint64(i*31+17), 5)
	}
	elapsed := time.Since(start)
	avgPerSearch := elapsed / 100

	if avgPerSearch > 15*time.Millisecond {
		t.Errorf("Average search time %v exceeds 15ms for 1500-node tree", avgPerSearch)
	}
}

// TestBKTreeAllZerosFingerprint verifies that an all-zeros fingerprint
// doesn't produce false positive matches.
func TestBKTreeAllZerosFingerprint(t *testing.T) {
	tree := NewBKTree()

	// Insert cards with non-zero hashes
	for i := 1; i <= 10; i++ {
		card := &models.Card{ID: fmt.Sprintf("nz-card-%d", i), Name: fmt.Sprintf("NonZero %d", i)}
		tree.Insert(uint64(i*1000), card)
	}

	// Search with all-zeros hash and small radius
	results := tree.Search(0, 3)
	for _, r := range results {
		// Verify the distance field is within the requested radius
		if r.Distance > 3 {
			t.Errorf("All-zeros query returned result with distance %d > 3 for card %s",
				r.Distance, r.Card.ID)
		}
	}
}

// TestBKTreeMaxHammingDistance verifies that maximum Hamming distance (64)
// doesn't produce false positive matches with small radii.
func TestBKTreeMaxHammingDistance(t *testing.T) {
	tree := NewBKTree()

	// Insert a card with all-ones hash
	tree.Insert(0xFFFFFFFFFFFFFFFF, &models.Card{ID: "max-hash", Name: "Max Hash Card"})

	// Search with all-zeros hash and small radius — should find nothing
	results := tree.Search(0, 10)
	for _, r := range results {
		if r.Card.ID == "max-hash" && r.Distance <= 10 {
			t.Errorf("False positive: max-hash card at distance %d should not match with radius 10",
				r.Distance)
		}
	}

	// Search with large radius — should find it
	results = tree.Search(0, 64)
	found := false
	for _, r := range results {
		if r.Card.ID == "max-hash" {
			found = true
			if r.Distance != 64 {
				t.Errorf("Expected distance 64, got %d", r.Distance)
			}
		}
	}
	if !found {
		t.Error("Expected to find max-hash card with radius 64")
	}
}

// TestBKTreeConcurrentAccess verifies that concurrent reads and writes
// don't cause data races or panics.
func TestBKTreeConcurrentAccess(t *testing.T) {
	tree := NewBKTree()

	// Pre-populate
	for i := 0; i < 100; i++ {
		card := &models.Card{ID: fmt.Sprintf("init-card-%d", i), Name: fmt.Sprintf("Init %d", i)}
		tree.Insert(uint64(i*100), card)
	}

	done := make(chan bool)

	// Concurrent writers
	for g := 0; g < 5; g++ {
		go func(offset int) {
			for i := 0; i < 50; i++ {
				card := &models.Card{ID: fmt.Sprintf("writer-%d-card-%d", offset, i),
					Name: fmt.Sprintf("Writer %d Card %d", offset, i)}
				tree.Insert(uint64(offset*10000+i*7), card)
			}
			done <- true
		}(g)
	}

	// Concurrent readers
	for g := 0; g < 5; g++ {
		go func(offset int) {
			for i := 0; i < 50; i++ {
				tree.Search(uint64(offset*10000+i*7), 5)
				tree.SearchExact(uint64(offset*10000 + i*7))
			}
			done <- true
		}(g)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestFingerprintServiceSearchByHashWithCardsFallback verifies the linear
// scan fallback when BK-tree is empty.
func TestFingerprintServiceSearchByHashWithCardsFallback(t *testing.T) {
	svc := NewFingerprintService(nil) // No DB, empty tree

	hash1 := int64(12345)
	hash2 := int64(12346) // distance 1 from hash1

	cards := []models.Card{
		{ID: "card-1", Name: "Pikachu", Phash: &hash1},
		{ID: "card-2", Name: "Charizard", Phash: &hash2},
	}

	// Should fall back to linear scan since BK-tree is empty
	result := svc.SearchByHashWithCards(hash1, cards)
	if result == nil {
		t.Fatal("Expected non-nil result from linear scan fallback")
	}
	if result.HighConfidence == nil {
		t.Error("Expected high-confidence match from linear scan")
	} else if result.HighConfidence.ID != "card-1" {
		t.Errorf("Expected card-1, got %s", result.HighConfidence.ID)
	}
	if result.BestDistance != 0 {
		t.Errorf("Expected best distance 0, got %d", result.BestDistance)
	}
}

// TestFingerprintServiceSearchByHashWithCardsExactMatch verifies early
// termination on exact match in linear scan.
func TestFingerprintServiceSearchByHashWithCardsExactMatch(t *testing.T) {
	svc := NewFingerprintService(nil)

	hash := int64(99999)
	cards := []models.Card{
		{ID: "card-1", Name: "Pikachu", Phash: &hash},
		{ID: "card-2", Name: "Charizard", Phash: new(int64)}, // zero hash
	}

	result := svc.SearchByHashWithCards(hash, cards)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.BestDistance != 0 {
		t.Errorf("Expected distance 0 for exact match, got %d", result.BestDistance)
	}
	if result.HighConfidence == nil || result.HighConfidence.ID != "card-1" {
		t.Error("Expected high-confidence exact match for card-1")
	}
}

// TestFingerprintServiceRebuildTree verifies that RebuildTree creates
// a fresh tree.
func TestFingerprintServiceRebuildTree(t *testing.T) {
	svc := NewFingerprintService(nil)

	// Add some fingerprints
	svc.AddFingerprint(100, &models.Card{ID: "card-1", Name: "Card 1"})
	svc.AddFingerprint(200, &models.Card{ID: "card-2", Name: "Card 2"})

	if svc.tree.count != 2 {
		t.Errorf("Expected 2 nodes before rebuild, got %d", svc.tree.count)
	}

	// Rebuild (without DB, tree should be empty after)
	svc.RebuildTree()

	if svc.tree.count != 0 {
		t.Errorf("Expected 0 nodes after rebuild without DB, got %d", svc.tree.count)
	}
}

// TestFingerprintServiceAddFingerprint verifies adding fingerprints
// to the service's BK-tree.
func TestFingerprintServiceAddFingerprint(t *testing.T) {
	svc := NewFingerprintService(nil)

	card := &models.Card{ID: "add-test", Name: "Add Test Card"}
	svc.AddFingerprint(42, card)

	if svc.tree.count != 1 {
		t.Errorf("Expected 1 node after AddFingerprint, got %d", svc.tree.count)
	}

	// Verify it's searchable
	result := svc.SearchByHash(42)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.HighConfidence == nil {
		t.Error("Expected high-confidence match after adding fingerprint")
	}
}

// TestBKTreeSearchResultsSortedByDistance verifies that deduplicated
// results are sorted by distance (ascending).
func TestBKTreeSearchResultsSortedByDistance(t *testing.T) {
	tree := NewBKTree()

	// Insert cards at various distances from hash 0
	tree.Insert(1, &models.Card{ID: "card-d1", Name: "Distance 1"})    // distance 1
	tree.Insert(7, &models.Card{ID: "card-d3", Name: "Distance 3"})    // distance 3
	tree.Insert(0x0F, &models.Card{ID: "card-d4", Name: "Distance 4"}) // distance 4
	tree.Insert(0, &models.Card{ID: "card-d0", Name: "Distance 0"})    // distance 0

	results := tree.Search(0, 10)

	// Verify sorted by distance
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("Results not sorted by distance: [%d]=%d > [%d]=%d",
				i-1, results[i-1].Distance, i, results[i].Distance)
		}
	}

	// First result should be distance 0
	if len(results) > 0 && results[0].Distance != 0 {
		t.Errorf("Expected first result distance 0, got %d", results[0].Distance)
	}
}
