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
	"crypto/sha256"
	"sync"
)

// OCRPoolSize is the number of concurrent Tesseract clients (SCAN-03).
// Configurable via environment, default 3. Defined here so it's accessible
// from all build variants.
var OCRPoolSize = 3

// ocrCache stores OCR results keyed by SHA-256 hash of input image bytes (SCAN-06).
// Shared between tesseract and stub implementations.
var ocrCache sync.Map

// ocrCacheEntry holds a cached OCR result (SCAN-06).
type ocrCacheEntry struct {
	Text         string
	DetectedCard string
}

// imageHash computes SHA-256 hash of image bytes for OCR caching (SCAN-06).
func imageHash(imgBytes []byte) [sha256.Size]byte {
	return sha256.Sum256(imgBytes)
}

// clearOCRCache removes all entries from the OCR cache.
// sync.Map has no Clear method, so we use Range+Delete.
func clearOCRCache() {
	ocrCache.Range(func(key, _ interface{}) bool {
		ocrCache.Delete(key)
		return true
	})
}
