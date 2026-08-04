package service

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"pokget/internal/models"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultOCRCacheEntries = 128
	ocrPipelineVersion     = "ocr-preprocess-v2"
)

// OCRPoolSize is the number of concurrent Tesseract clients.
var OCRPoolSize = 3

type ocrCacheEntry struct {
	Text           string
	DetectedCard   string
	ProcessedImage []byte
}

type ocrCacheItem struct {
	key   any
	value any
}

// boundedOCRCache is a concurrency-safe LRU with the subset of sync.Map's API
// used by the OCR implementations and tests.
type boundedOCRCache struct {
	mu       sync.Mutex
	capacity int
	items    map[any]*list.Element
	order    *list.List
}

func newBoundedOCRCache(capacity int) *boundedOCRCache {
	if capacity < 1 {
		capacity = 1
	}
	return &boundedOCRCache{
		capacity: capacity,
		items:    make(map[any]*list.Element, capacity),
		order:    list.New(),
	}
}

var ocrCache = newBoundedOCRCache(defaultOCRCacheEntries)

func (cache *boundedOCRCache) Store(key, value any) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	value = cloneOCRCacheValue(value)
	if element, ok := cache.items[key]; ok {
		element.Value.(*ocrCacheItem).value = value
		cache.order.MoveToFront(element)
		return
	}

	element := cache.order.PushFront(&ocrCacheItem{key: key, value: value})
	cache.items[key] = element
	if cache.order.Len() <= cache.capacity {
		return
	}
	oldest := cache.order.Back()
	delete(cache.items, oldest.Value.(*ocrCacheItem).key)
	cache.order.Remove(oldest)
}

func (cache *boundedOCRCache) Load(key any) (any, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	element, ok := cache.items[key]
	if !ok {
		return nil, false
	}
	cache.order.MoveToFront(element)
	return cloneOCRCacheValue(element.Value.(*ocrCacheItem).value), true
}

func (cache *boundedOCRCache) Delete(key any) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if element, ok := cache.items[key]; ok {
		delete(cache.items, key)
		cache.order.Remove(element)
	}
}

func (cache *boundedOCRCache) Range(fn func(key, value any) bool) {
	cache.mu.Lock()
	items := make([]ocrCacheItem, 0, len(cache.items))
	for element := cache.order.Front(); element != nil; element = element.Next() {
		item := element.Value.(*ocrCacheItem)
		items = append(items, ocrCacheItem{key: item.key, value: cloneOCRCacheValue(item.value)})
	}
	cache.mu.Unlock()

	for _, item := range items {
		if !fn(item.key, item.value) {
			return
		}
	}
}

func (cache *boundedOCRCache) Len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.items)
}

func (cache *boundedOCRCache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.items = make(map[any]*list.Element, cache.capacity)
	cache.order.Init()
}

func cloneOCRCacheValue(value any) any {
	entry, ok := value.(ocrCacheEntry)
	if !ok {
		return value
	}
	entry.ProcessedImage = append([]byte(nil), entry.ProcessedImage...)
	return entry
}

func imageHash(imgBytes []byte) [sha256.Size]byte {
	return sha256.Sum256(imgBytes)
}

type ocrCacheKey struct {
	Image    [sha256.Size]byte
	Language string
	Corpus   [sha256.Size]byte
	Options  [sha256.Size]byte
}

func makeOCRCacheKey(imgBytes []byte, lang string, cards []models.Card) ocrCacheKey {
	return makeOCRCacheKeyWithConfig(imgBytes, lang, cards, normalizeOCRScanConfig(OCRScanConfig{}))
}

func makeOCRCacheKeyWithConfig(imgBytes []byte, lang string, cards []models.Card, config OCRScanConfig) ocrCacheKey {
	keys := make([]string, 0, len(cards))
	for _, card := range cards {
		keys = append(keys, card.ID+"\x00"+card.Name+"\x00"+card.Game+"\x00"+card.Language)
	}
	sort.Strings(keys)

	corpusHasher := sha256.New()
	for _, key := range keys {
		_, _ = corpusHasher.Write([]byte(key))
		_, _ = corpusHasher.Write([]byte{0xff})
	}
	var corpus [sha256.Size]byte
	copy(corpus[:], corpusHasher.Sum(nil))

	config = normalizeOCRScanConfig(config)
	formats := append([]string(nil), config.AllowedFormats...)
	sort.Strings(formats)
	var options strings.Builder
	options.WriteString(ocrPipelineVersion)
	options.WriteByte(0)
	options.WriteString(config.Game)
	options.WriteByte(0)
	options.WriteString(strconv.FormatBool(config.UseLayoutROIs))
	options.WriteByte(0)
	options.WriteString(fmt.Sprintf("%d:%d", config.MaxInputBytes, config.MaxPixels))
	options.WriteByte(0)
	options.WriteString(strings.Join(formats, ","))
	if config.GuideCrop != nil {
		options.WriteByte(0)
		for _, value := range []float64{config.GuideCrop.MinX, config.GuideCrop.MinY, config.GuideCrop.MaxX, config.GuideCrop.MaxY} {
			options.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
			options.WriteByte(':')
		}
	}

	return ocrCacheKey{
		Image:    imageHash(imgBytes),
		Language: lang,
		Corpus:   corpus,
		Options:  sha256.Sum256([]byte(options.String())),
	}
}

func clearOCRCache() {
	ocrCache.Clear()
}
