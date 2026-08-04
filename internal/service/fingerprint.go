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
	"errors"
	"fmt"
	"image"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"pokget/internal/models"

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
	if card == nil {
		return
	}
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

// Len returns the number of indexed fingerprints without racing concurrent
// Insert calls.
func (t *BKTree) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
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
		if m.Card == nil || m.Card.ID == "" {
			continue
		}
		if existing, ok := best[m.Card.ID]; !ok || m.Distance < existing.Distance {
			best[m.Card.ID] = m
		}
	}

	result := make([]FingerprintMatch, 0, len(best))
	for _, m := range best {
		result = append(result, m)
	}

	// Sort by distance and stable printing ID for deterministic ties.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Distance != result[j].Distance {
			return result[i].Distance < result[j].Distance
		}
		return result[i].Card.ID < result[j].Card.ID
	})

	return result
}

const (
	defaultFingerprintAlgorithm = "phash64"
	defaultFingerprintVersion   = 1
)

// FingerprintIndexScope identifies an algorithm-version index for one TCG.
// The TCG is mandatory in typed matching paths, preventing cross-catalog hash
// collisions from becoming candidates.
type FingerprintIndexScope struct {
	TCG       models.TCG
	Language  models.Language
	Algorithm string
	Version   int
}

func (scope FingerprintIndexScope) normalized() FingerprintIndexScope {
	scope.Algorithm = strings.ToLower(strings.TrimSpace(scope.Algorithm))
	if scope.Algorithm == "" {
		scope.Algorithm = defaultFingerprintAlgorithm
	}
	if scope.Version == 0 {
		scope.Version = defaultFingerprintVersion
	}
	return scope
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
	scopedTrees    map[FingerprintIndexScope]*BKTree
	mu             sync.RWMutex
	PhashHighConf  int // Strict threshold for high-confidence matches (SCAN-02)
	PhashPotential int // Relaxed threshold for potential matches (SCAN-02)
}

// NewFingerprintService creates a new FingerprintService, loads all existing
// fingerprints from the database into the BK-tree (SCAN-01, SCAN-12).
func NewFingerprintService(db *sql.DB) *FingerprintService {
	svc := &FingerprintService{
		db:             db,
		tree:           NewBKTree(),
		scopedTrees:    make(map[FingerprintIndexScope]*BKTree),
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
	rows, err := s.db.Query(`
		SELECT id, name, set_name, price_usd, price_eur, image_url, variant,
		       change_24h, phash, game, language, rarity
		FROM (
		    SELECT card.id, card.name, card.set_name,
		           COALESCE(card.price_usd, 0) AS price_usd,
		           COALESCE(card.price_eur, 0) AS price_eur,
		           COALESCE(card.image_url, '') AS image_url,
		           COALESCE(card.variant, '') AS variant,
		           COALESCE(card.change_24h, 0) AS change_24h,
		           card.phash AS phash,
		           COALESCE(card.game, '') AS game,
		           COALESCE(card.language, '') AS language,
		           COALESCE(card.rarity, '') AS rarity
		    FROM cards AS card
		    WHERE card.phash IS NOT NULL
		      AND card.superseded_by_card_id IS NULL
		      AND (card.source_id IS NULL OR card.catalog_active = TRUE)

		    UNION ALL

		    SELECT card.id, card.name, card.set_name, COALESCE(card.price_usd, 0), COALESCE(card.price_eur, 0),
		           COALESCE(card.image_url, ''), COALESCE(card.variant, ''), COALESCE(card.change_24h, 0), fingerprint.hash,
		           COALESCE(card.game, '') AS game,
		           COALESCE(card.language, '') AS language,
		           COALESCE(card.rarity, '') AS rarity
		    FROM card_fingerprints AS fingerprint
		    JOIN card_images AS image ON image.id = fingerprint.image_id
		    JOIN cards AS card ON card.id = image.card_id
		    WHERE card.catalog_active = TRUE
		      AND image.status = 'ready'
		      AND fingerprint.algorithm = 'phash64'
		      AND fingerprint.algorithm_version = 1
	) AS stored_fingerprints`)
	if err != nil {
		slog.Error("Fingerprint: Failed to load fingerprints from DB", "error", err)
		return
	}
	defer rows.Close()

	loaded := 0
	for rows.Next() {
		var c models.Card
		var phash sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.ImageURL, &c.Variant, &c.Change24h, &phash, &c.Game, &c.Language, &c.Rarity); err != nil {
			slog.Warn("Failed to scan fingerprint row, skipping", "error", err)
			continue
		}
		if phash.Valid {
			c.Phash = &phash.Int64
			s.addFingerprintLocked(fingerprintScopeForCard(c), uint64(phash.Int64), &c) // #nosec G115
			loaded++
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Fingerprint: Failed while reading stored fingerprints", "error", err)
	}
	slog.Info("Fingerprint: Loaded fingerprints into BK-tree", "count", loaded)
}

// AddFingerprint adds a new fingerprint for a card to the BK-tree (SCAN-12).
// This allows multiple fingerprints per card (e.g., different art variants).
func (s *FingerprintService) AddFingerprint(hash uint64, card *models.Card) {
	if card == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addFingerprintLocked(fingerprintScopeForCard(*card), hash, card)
}

// AddFingerprintScoped indexes a fingerprint in the global compatibility tree
// and an explicit TCG/algorithm/version tree.
func (s *FingerprintService) AddFingerprintScoped(scope FingerprintIndexScope, hash uint64, card *models.Card) error {
	if card == nil || card.ID == "" {
		return errors.New("fingerprint: card and stable card ID are required")
	}
	scope = scope.normalized()
	if !scope.TCG.Valid() {
		return errors.New("fingerprint: valid TCG scope is required")
	}
	if !scope.Language.Valid() {
		return errors.New("fingerprint: valid language scope is required")
	}
	if scope.Version < 1 {
		return errors.New("fingerprint: algorithm version must be positive")
	}
	if cardTCG := tcgForCard(*card); cardTCG != models.TCGUnknown && cardTCG != scope.TCG {
		return fmt.Errorf("fingerprint: card TCG %q does not match index TCG %q", cardTCG, scope.TCG)
	}
	if cardLanguage, err := models.ParseLanguage(card.Language); err == nil && cardLanguage != scope.Language {
		return fmt.Errorf("fingerprint: card language %q does not match index language %q", cardLanguage, scope.Language)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addFingerprintLocked(scope, hash, card)
	return nil
}

func (s *FingerprintService) addFingerprintLocked(scope FingerprintIndexScope, hash uint64, card *models.Card) {
	if card == nil {
		return
	}
	scope = scope.normalized()
	if scope.Algorithm == defaultFingerprintAlgorithm && scope.Version == defaultFingerprintVersion {
		s.tree.Insert(hash, card)
	}
	if !scope.TCG.Valid() || !scope.Language.Valid() {
		return
	}
	tree := s.scopedTrees[scope]
	if tree == nil {
		tree = NewBKTree()
		s.scopedTrees[scope] = tree
	}
	tree.Insert(hash, card)
}

// RebuildTree reloads all fingerprints from the database and rebuilds the BK-tree.
func (s *FingerprintService) RebuildTree() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tree = NewBKTree()
	s.scopedTrees = make(map[FingerprintIndexScope]*BKTree)
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchByHashLocked(hashVal)
}

func (s *FingerprintService) searchByHashLocked(hashVal int64) *MatchResult {
	return s.searchTree(s.tree, hashVal)
}

func (s *FingerprintService) searchTree(tree *BKTree, hashVal int64) *MatchResult {
	potential := tree.Search(uint64(hashVal), s.PhashPotential) // #nosec G115
	return s.resultFromPotential(potential)
}

func (s *FingerprintService) resultFromPotential(potential []FingerprintMatch) *MatchResult {
	potential = deduplicateByCard(potential)
	result := &MatchResult{Potential: potential, BestDistance: 64}
	for _, match := range potential {
		if match.Distance < result.BestDistance {
			result.BestDistance = match.Distance
		}
		if match.Distance <= s.PhashHighConf && result.HighConfidence == nil {
			result.HighConfidence = match.Card
		}
	}
	return result
}

// SearchByHashWithCards uses linear scan when BK-tree is empty, falling back
// to the provided card list (SCAN-01 fallback).
func (s *FingerprintService) SearchByHashWithCards(hashVal int64, cards []models.Card) *MatchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try BK-tree first
	if s.tree.Len() > 0 {
		indexed := s.searchByHashLocked(hashVal)
		if cards == nil {
			return indexed
		}
		return s.filterIndexedMatches(indexed, cards)
	}
	return s.linearSearch(hashVal, cards)
}

func (s *FingerprintService) filterIndexedMatches(indexed *MatchResult, cards []models.Card) *MatchResult {
	allowed := make(map[string]*models.Card, len(cards))
	for index := range cards {
		if cards[index].ID != "" {
			allowed[cards[index].ID] = &cards[index]
		}
	}
	potential := make([]FingerprintMatch, 0, len(indexed.Potential))
	for _, match := range indexed.Potential {
		if match.Card == nil {
			continue
		}
		card, ok := allowed[match.Card.ID]
		if !ok {
			continue
		}
		potential = append(potential, FingerprintMatch{Card: card, Distance: match.Distance})
	}
	return s.resultFromPotential(potential)
}

func (s *FingerprintService) linearSearch(hashVal int64, cards []models.Card) *MatchResult {
	targetHash := goimagehash.NewImageHash(uint64(hashVal), goimagehash.PHash) // #nosec G115
	potential := make([]FingerprintMatch, 0)
	for index := range cards {
		card := &cards[index]
		if card.ID == "" || card.Phash == nil {
			continue
		}
		storedHash := goimagehash.NewImageHash(uint64(*card.Phash), goimagehash.PHash) // #nosec G115
		distance, err := targetHash.Distance(storedHash)
		if err != nil || distance > s.PhashPotential {
			continue
		}
		potential = append(potential, FingerprintMatch{Card: card, Distance: distance})
	}
	return s.resultFromPotential(potential)
}

// SearchByHashWithScope searches only the selected TCG and fingerprint index.
// The supplied cards are filtered again so callers cannot accidentally admit a
// cross-TCG, wrong-language, or inactive printing through a populated index.
func (s *FingerprintService) SearchByHashWithScope(hashVal int64, scope FingerprintIndexScope, cards []models.Card) *MatchResult {
	scope = scope.normalized()
	if !scope.TCG.Valid() || !scope.Language.Valid() {
		return &MatchResult{BestDistance: 64}
	}
	allowedCards := make([]models.Card, 0, len(cards))
	for index := range cards {
		if cards[index].IsCatalogActive() && tcgForCard(cards[index]) == scope.TCG && scope.Language.Matches(cards[index].Language) {
			allowedCards = append(allowedCards, cards[index])
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	tree := s.scopedTrees[scope]
	if tree == nil || tree.Len() == 0 {
		if scope.Algorithm != defaultFingerprintAlgorithm || scope.Version != defaultFingerprintVersion {
			return &MatchResult{BestDistance: 64}
		}
		return s.linearSearch(hashVal, allowedCards)
	}
	return s.filterIndexedMatches(s.searchTree(tree, hashVal), allowedCards)
}

func tcgForCard(card models.Card) models.TCG {
	tcg, err := models.ParseTCG(card.Game)
	if err != nil {
		return models.TCGUnknown
	}
	return tcg
}

func fingerprintScopeForCard(card models.Card) FingerprintIndexScope {
	language, _ := models.ParseLanguage(card.Language)
	return FingerprintIndexScope{TCG: tcgForCard(card), Language: language}
}
