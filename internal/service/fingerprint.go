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
	"database/sql"
	"fmt"
	"image"
	"log/slog"
	"pokget/internal/models"
	"sort"
	"sync"

	"github.com/corona10/goimagehash"
)

// DefaultPhashThresholdHighConf is the strict threshold for high-confidence
// fingerprint matches (SCAN-02).
const DefaultPhashThresholdHighConf = 5

// DefaultPhashThresholdPotential is the relaxed threshold for potential matches
// that need secondary verification via OCR or LLM (SCAN-02).
const DefaultPhashThresholdPotential = 10

// FingerprintMatch represents a single match result from fingerprint search,
// supporting multiple fingerprints per card (SCAN-12).
type FingerprintMatch struct {
	Card     *models.Card
	Distance int
}

// BKTree implements a Burkhard-Keller tree for efficient Hamming distance
// search on perceptual hashes (SCAN-01).
type BKTree struct {
	mu       sync.RWMutex
	root     *bkNode
	count    int
	distance func(uint64, uint64) int
}

// bkNode is a node in the BK-tree.
type bkNode struct {
	hash     uint64
	cardID   string
	card     *models.Card
	children map[int]*bkNode
}

// NewBKTree creates a new BK-tree using the provided distance function.
func NewBKTree() *BKTree {
	return &BKTree{
		distance: hammingDistance,
	}
}

// Insert adds a hash with associated card info into the BK-tree (SCAN-01, SCAN-12).
func (t *BKTree) Insert(hash uint64, card *models.Card) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := &bkNode{
		hash:   hash,
		cardID: card.ID,
		card:   card,
	}

	if t.root == nil {
		t.root = node
		t.count++
		return
	}

	current := t.root
	for {
		d := t.distance(current.hash, hash)
		if d == 0 && current.cardID == card.ID {
			// Duplicate hash for same card, skip
			return
		}
		if current.children == nil {
			current.children = make(map[int]*bkNode)
		}
		if child, ok := current.children[d]; ok {
			current = child
		} else {
			current.children[d] = node
			t.count++
			return
		}
	}
}

// Search returns all hashes within the given radius from the query hash (SCAN-01).
// Results are deduplicated by card ID, keeping the best distance per card (SCAN-12).
func (t *BKTree) Search(query uint64, radius int) []FingerprintMatch {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return nil
	}

	var results []FingerprintMatch
	t.searchNode(t.root, query, radius, &results)

	// Deduplicate by card ID, keeping best distance (SCAN-12)
	return deduplicateByCard(results)
}

// SearchExact checks for an exact match (distance=0) and returns immediately
// if found (SCAN-14 early termination).
func (t *BKTree) SearchExact(query uint64) *FingerprintMatch {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return nil
	}

	// Check root
	d := t.distance(t.root.hash, query)
	if d == 0 {
		return &FingerprintMatch{Card: t.root.card, Distance: 0}
	}

	// For exact match, only need to explore children at distance d from current
	current := t.root
	for current != nil {
		d = t.distance(current.hash, query)
		if d == 0 {
			return &FingerprintMatch{Card: current.card, Distance: 0}
		}
		if current.children == nil {
			break
		}
		// For exact match search, we only follow the child at distance d
		// because we need distance 0, so |d - child_dist| <= 0 means child_dist == d
		current = current.children[d]
	}

	return nil
}

func (t *BKTree) searchNode(node *bkNode, query uint64, radius int, results *[]FingerprintMatch) {
	d := t.distance(node.hash, query)

	if d <= radius {
		*results = append(*results, FingerprintMatch{Card: node.card, Distance: d})
	}

	// Only explore children within [d-radius, d+radius]
	low := d - radius
	if low < 0 {
		low = 0
	}
	high := d + radius

	for dist, child := range node.children {
		if dist >= low && dist <= high {
			t.searchNode(child, query, radius, results)
		}
	}
}

// deduplicateByCard keeps only the best (lowest distance) match per card ID (SCAN-12).
func deduplicateByCard(matches []FingerprintMatch) []FingerprintMatch {
	best := make(map[string]FingerprintMatch)
	for _, m := range matches {
		if existing, ok := best[m.Card.ID]; !ok || m.Distance < existing.Distance {
			best[m.Card.ID] = m
		}
	}

	result := make([]FingerprintMatch, 0, len(best))
	for _, m := range best {
		result = append(result, m)
	}

	// Sort by distance (best first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Distance < result[j].Distance
	})

	return result
}

// hammingDistance computes the Hamming distance between two uint64 hashes.
func hammingDistance(a, b uint64) int {
	xor := a ^ b
	count := 0
	for xor != 0 {
		count++
		xor &= xor - 1 // Clear lowest set bit
	}
	return count
}

// FingerprintService provides perceptual hash matching with BK-tree acceleration.
type FingerprintService struct {
	db             *sql.DB
	tree           *BKTree
	PhashHighConf  int // Strict threshold for high-confidence matches (SCAN-02)
	PhashPotential int // Relaxed threshold for potential matches (SCAN-02)
}

// NewFingerprintService creates a new FingerprintService, loads all existing
// fingerprints from the database into the BK-tree (SCAN-01, SCAN-12).
func NewFingerprintService(db *sql.DB) *FingerprintService {
	svc := &FingerprintService{
		db:             db,
		tree:           NewBKTree(),
		PhashHighConf:  DefaultPhashThresholdHighConf,
		PhashPotential: DefaultPhashThresholdPotential,
	}

	if db != nil {
		svc.loadFingerprintsFromDB()
	}

	return svc
}

// loadFingerprintsFromDB loads all stored fingerprints into the BK-tree,
// supporting multiple fingerprints per card (SCAN-01, SCAN-12).
func (s *FingerprintService) loadFingerprintsFromDB() {
	rows, err := s.db.Query("SELECT id, name, set_name, price_usd, price_eur, image_url, variant, change_24h, phash, game FROM cards WHERE phash IS NOT NULL")
	if err != nil {
		slog.Error("Fingerprint: Failed to load fingerprints from DB", "error", err)
		return
	}
	defer rows.Close()

	loaded := 0
	for rows.Next() {
		var c models.Card
		var phash sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &phash, &c.Game); err != nil {
			continue
		}
		if phash.Valid {
			c.Phash = &phash.Int64
			s.tree.Insert(uint64(phash.Int64), &c) // #nosec G115
			loaded++
		}
	}
	slog.Info("Fingerprint: Loaded fingerprints into BK-tree", "count", loaded)
}

// AddFingerprint adds a new fingerprint for a card to the BK-tree (SCAN-12).
// This allows multiple fingerprints per card (e.g., different art variants).
func (s *FingerprintService) AddFingerprint(hash uint64, card *models.Card) {
	s.tree.Insert(hash, card)
}

// RebuildTree reloads all fingerprints from the database and rebuilds the BK-tree.
func (s *FingerprintService) RebuildTree() {
	s.tree = NewBKTree()
	if s.db != nil {
		s.loadFingerprintsFromDB()
	}
}

// CalculateHash computes the perceptual hash (pHash) of an image.
func (s *FingerprintService) CalculateHash(img image.Image) (int64, error) {
	if img == nil {
		return 0, fmt.Errorf("fingerprint: cannot calculate hash of nil image")
	}
	hash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate pHash: %w", err)
	}
	return int64(hash.GetHash()), nil // #nosec G115 - conversion for BIGINT storage
}

// MatchFingerprint searches for a card with a similar pHash in the provided list.
// Uses linear scan (backward-compatible). For BK-tree search, use SearchByHash.
func (s *FingerprintService) MatchFingerprint(hashVal int64, cards []models.Card) (*models.Card, int, error) {
	var bestMatch *models.Card
	minDistance := 64 // Max bits in pHash

	targetHash := goimagehash.NewImageHash(uint64(hashVal), goimagehash.PHash) // #nosec G115

	for _, c := range cards {
		if c.Phash == nil {
			continue
		}

		storedHash := goimagehash.NewImageHash(uint64(*c.Phash), goimagehash.PHash) // #nosec G115
		distance, err := targetHash.Distance(storedHash)
		if err != nil {
			continue
		}

		// SCAN-14: Early termination on exact match
		if distance == 0 {
			return &c, 0, nil
		}

		if distance < minDistance {
			minDistance = distance
			bestMatch = &c
		}
	}

	// SCAN-02: Use configurable threshold (high-confidence)
	if minDistance > s.PhashHighConf {
		return nil, minDistance, nil
	}

	return bestMatch, minDistance, nil
}

// MatchResult contains the result of a two-tier fingerprint match (SCAN-02).
type MatchResult struct {
	HighConfidence *models.Card       // Match within strict threshold
	Potential      []FingerprintMatch // Matches within relaxed threshold (need verification)
	BestDistance   int
}

// SearchByHash uses the BK-tree for efficient search (SCAN-01).
// Returns a two-tier result: high-confidence matches and potential matches (SCAN-02).
func (s *FingerprintService) SearchByHash(hashVal int64) *MatchResult {
	query := uint64(hashVal) // #nosec G115

	// SCAN-14: Check for exact match first (instant return)
	if exact := s.tree.SearchExact(query); exact != nil {
		return &MatchResult{
			HighConfidence: exact.Card,
			BestDistance:   0,
		}
	}

	// Search with relaxed threshold for potential matches
	potential := s.tree.Search(query, s.PhashPotential)

	result := &MatchResult{
		Potential:    potential,
		BestDistance: 64,
	}

	// Find best high-confidence match
	for _, m := range potential {
		if m.Distance < result.BestDistance {
			result.BestDistance = m.Distance
		}
		if m.Distance <= s.PhashHighConf {
			result.HighConfidence = m.Card
		}
	}

	if len(potential) == 0 {
		result.BestDistance = 64
	}

	return result
}

// SearchByHashWithCards uses linear scan when BK-tree is empty, falling back
// to the provided card list (SCAN-01 fallback).
func (s *FingerprintService) SearchByHashWithCards(hashVal int64, cards []models.Card) *MatchResult {
	// Try BK-tree first
	if s.tree.count > 0 {
		return s.SearchByHash(hashVal)
	}

	// Fallback to linear scan
	result := &MatchResult{
		BestDistance: 64,
	}

	targetHash := goimagehash.NewImageHash(uint64(hashVal), goimagehash.PHash) // #nosec G115

	// Track best distance per card for dedup (SCAN-12)
	bestByCard := make(map[string]int)

	for _, c := range cards {
		if c.Phash == nil {
			continue
		}

		storedHash := goimagehash.NewImageHash(uint64(*c.Phash), goimagehash.PHash) // #nosec G115
		distance, err := targetHash.Distance(storedHash)
		if err != nil {
			continue
		}

		// SCAN-14: Early termination on exact match
		if distance == 0 {
			return &MatchResult{
				HighConfidence: &c,
				BestDistance:   0,
			}
		}

		// Keep best distance per card (SCAN-12)
		if existing, ok := bestByCard[c.ID]; !ok || distance < existing {
			bestByCard[c.ID] = distance
			if distance <= s.PhashPotential {
				result.Potential = append(result.Potential, FingerprintMatch{
					Card:     &c,
					Distance: distance,
				})
			}
		}
	}

	// Sort potential matches by distance
	sort.Slice(result.Potential, func(i, j int) bool {
		return result.Potential[i].Distance < result.Potential[j].Distance
	})

	// Deduplicate
	result.Potential = deduplicateByCard(result.Potential)

	// Find best high-confidence match
	for _, m := range result.Potential {
		if m.Distance < result.BestDistance {
			result.BestDistance = m.Distance
		}
		if m.Distance <= s.PhashHighConf && result.HighConfidence == nil {
			result.HighConfidence = m.Card
		}
	}

	if len(result.Potential) == 0 {
		result.BestDistance = 64
	}

	return result
}
