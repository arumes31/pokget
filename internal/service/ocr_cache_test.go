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
	"pokget/internal/models"
	"testing"
)

// --- SCAN-06: OCR cache tests ---

func TestImageHash(t *testing.T) {
	// Same input should produce same hash
	data1 := []byte("test image data")
	hash1 := imageHash(data1)
	hash2 := imageHash(data1)
	if hash1 != hash2 {
		t.Error("Expected same hash for same input")
	}

	// Different input should produce different hash
	data2 := []byte("different image data")
	hash3 := imageHash(data2)
	if hash1 == hash3 {
		t.Error("Expected different hashes for different inputs")
	}

	// Empty input should still produce a valid hash
	emptyHash := imageHash([]byte{})
	if len(emptyHash) != 32 { // SHA-256 produces 32 bytes
		t.Errorf("Expected 32-byte hash, got %d bytes", len(emptyHash))
	}
}

func TestOCRCacheStoreAndLoad(t *testing.T) {
	// Clear cache before test
	clearOCRCache()

	// Store a result
	h := imageHash([]byte("test"))
	key := string(h[:]) + "eng"
	ocrCache.Store(key, ocrCacheEntry{
		Text:         "Pikachu",
		DetectedCard: "Pikachu",
	})

	// Load the result
	cached, ok := ocrCache.Load(key)
	if !ok {
		t.Fatal("Expected cache hit")
	}

	entry := cached.(ocrCacheEntry)
	if entry.Text != "Pikachu" {
		t.Errorf("Expected cached text 'Pikachu', got %q", entry.Text)
	}
	if entry.DetectedCard != "Pikachu" {
		t.Errorf("Expected cached card 'Pikachu', got %q", entry.DetectedCard)
	}
}

func TestOCRCacheMiss(t *testing.T) {
	clearOCRCache()

	_, ok := ocrCache.Load("nonexistent-key")
	if ok {
		t.Error("Expected cache miss for nonexistent key")
	}
}

func TestOCRCacheOverwrite(t *testing.T) {
	clearOCRCache()

	h := imageHash([]byte("overwrite-test"))
	key := string(h[:]) + "eng"

	// Store first result
	ocrCache.Store(key, ocrCacheEntry{
		Text:         "First",
		DetectedCard: "Card1",
	})

	// Overwrite with second result
	ocrCache.Store(key, ocrCacheEntry{
		Text:         "Second",
		DetectedCard: "Card2",
	})

	cached, ok := ocrCache.Load(key)
	if !ok {
		t.Fatal("Expected cache hit")
	}

	entry := cached.(ocrCacheEntry)
	if entry.Text != "Second" {
		t.Errorf("Expected overwritten text 'Second', got %q", entry.Text)
	}
	if entry.DetectedCard != "Card2" {
		t.Errorf("Expected overwritten card 'Card2', got %q", entry.DetectedCard)
	}
}

func TestOCRCacheDifferentLanguages(t *testing.T) {
	clearOCRCache()

	imgData := []byte("same-image-data")
	hash := imageHash(imgData)
	keyEng := string(hash[:]) + "eng"
	keyJpn := string(hash[:]) + "jpn"

	ocrCache.Store(keyEng, ocrCacheEntry{Text: "English text", DetectedCard: "Pikachu"})
	ocrCache.Store(keyJpn, ocrCacheEntry{Text: "日本語テキスト", DetectedCard: "ピカチュウ"})

	cachedEng, okEng := ocrCache.Load(keyEng)
	cachedJpn, okJpn := ocrCache.Load(keyJpn)

	if !okEng || !okJpn {
		t.Fatal("Expected cache hits for both languages")
	}

	entryEng := cachedEng.(ocrCacheEntry)
	entryJpn := cachedJpn.(ocrCacheEntry)

	if entryEng.DetectedCard != "Pikachu" {
		t.Errorf("Expected English card 'Pikachu', got %q", entryEng.DetectedCard)
	}
	if entryJpn.DetectedCard != "ピカチュウ" {
		t.Errorf("Expected Japanese card 'ピカチュウ', got %q", entryJpn.DetectedCard)
	}
}

func TestOCRCacheKeyIncludesCandidateCorpus(t *testing.T) {
	imageBytes := []byte("same image")
	one := []models.Card{{ID: "one", Name: "Pikachu", Game: "pokemon", Language: "en"}}
	two := []models.Card{{ID: "two", Name: "Charizard", Game: "pokemon", Language: "en"}}

	if makeOCRCacheKey(imageBytes, "eng", one) == makeOCRCacheKey(imageBytes, "eng", two) {
		t.Fatal("cache keys must change when the candidate corpus changes")
	}
	if makeOCRCacheKey(imageBytes, "eng", one) != makeOCRCacheKey(imageBytes, "eng", append([]models.Card(nil), one...)) {
		t.Fatal("equivalent candidate corpora must produce stable cache keys")
	}
}

func TestOCRCacheKeyIgnoresCandidateOrder(t *testing.T) {
	imageBytes := []byte("same image")
	first := []models.Card{{ID: "one", Name: "Pikachu"}, {ID: "two", Name: "Charizard"}}
	second := []models.Card{first[1], first[0]}

	if makeOCRCacheKey(imageBytes, "eng", first) != makeOCRCacheKey(imageBytes, "eng", second) {
		t.Fatal("candidate ordering must not invalidate the OCR cache")
	}
}

// --- SCAN-03: OCR pool size test ---

func TestOCRPoolSizeDefault(t *testing.T) {
	// Verify default pool size
	if OCRPoolSize != 3 {
		t.Errorf("Expected default OCRPoolSize 3, got %d", OCRPoolSize)
	}
}

func TestBoundedOCRCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newBoundedOCRCache(2)
	cache.Store("first", ocrCacheEntry{Text: "first"})
	cache.Store("second", ocrCacheEntry{Text: "second"})
	if _, ok := cache.Load("first"); !ok {
		t.Fatal("expected first entry")
	}
	cache.Store("third", ocrCacheEntry{Text: "third"})
	if _, ok := cache.Load("second"); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	if cache.Len() != 2 {
		t.Fatalf("cache length = %d, want 2", cache.Len())
	}
}

func TestBoundedOCRCacheDefensivelyCopiesProcessedImage(t *testing.T) {
	cache := newBoundedOCRCache(1)
	processed := []byte{1, 2, 3}
	cache.Store("key", ocrCacheEntry{ProcessedImage: processed})
	processed[0] = 9

	loaded, ok := cache.Load("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	entry := loaded.(ocrCacheEntry)
	if entry.ProcessedImage[0] != 1 {
		t.Fatal("store retained caller-owned image buffer")
	}
	entry.ProcessedImage[0] = 8
	loaded, _ = cache.Load("key")
	if loaded.(ocrCacheEntry).ProcessedImage[0] != 1 {
		t.Fatal("load returned cache-owned image buffer")
	}
}

func TestOCRCacheKeyIncludesPreprocessingOptions(t *testing.T) {
	data := []byte("same image")
	cards := []models.Card{{ID: "sv1-001", Name: "Example", Game: "pokemon"}}
	defaultKey := makeOCRCacheKeyWithConfig(data, "eng", cards, OCRScanConfig{})
	gameKey := makeOCRCacheKeyWithConfig(data, "eng", cards, OCRScanConfig{Game: "pokemon", UseLayoutROIs: true})
	cropKey := makeOCRCacheKeyWithConfig(data, "eng", cards, OCRScanConfig{
		GuideCrop: &OCRNormalizedRect{MinX: 0.1, MinY: 0.1, MaxX: 0.9, MaxY: 0.9},
	})
	if defaultKey == gameKey || defaultKey == cropKey || gameKey == cropKey {
		t.Fatal("preprocessing options must be part of the cache key")
	}
}
