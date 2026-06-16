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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type ImageCacheService struct {
	BaseDir    string
	maxAge     time.Duration // BUG-H07: Maximum age for cached images
	stopCh     chan struct{}
	httpClient *http.Client
}

func NewImageCacheService(baseDir string) *ImageCacheService {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0700); err != nil { // #nosec G301 - restricted permissions
		slog.Error("Failed to create image cache directory", "dir", baseDir, "error", err)
	}
	svc := &ImageCacheService{
		BaseDir:    baseDir,
		maxAge:     24 * time.Hour, // Default: evict entries older than 24 hours
		stopCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	// BUG-H07 FIX: Start background goroutine to periodically clean up stale cache entries
	go svc.cleanupStaleEntries()

	return svc
}

// Close stops the background cleanup goroutine.
func (s *ImageCacheService) Close() {
	close(s.stopCh)
}

// cleanupStaleEntries periodically removes cached image files older than maxAge
func (s *ImageCacheService) cleanupStaleEntries() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			entries, err := os.ReadDir(s.BaseDir)
			if err != nil {
				continue
			}
			now := time.Now()
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if now.Sub(info.ModTime()) > s.maxAge {
					path := filepath.Join(s.BaseDir, entry.Name())
					_ = os.Remove(path)
					slog.Info("ImageCache: Evicted stale entry", "file", entry.Name())
				}
			}
		case <-s.stopCh:
			return
		}
	}
}

// GetImagePath returns the local path for a card image, downloading it if necessary
func (s *ImageCacheService) GetImagePath(cardID string, remoteURL string) (string, error) {
	// Sanitize cardID to prevent directory traversal
	safeID := filepath.Base(cardID)
	localPath := filepath.Join(s.BaseDir, safeID+".png")

	// Check if already exists and is not stale
	if info, err := os.Stat(localPath); err == nil {
		// BUG-H07 FIX: Check if the cached file has expired
		if time.Since(info.ModTime()) > s.maxAge {
			_ = os.Remove(localPath)
			slog.Info("ImageCache: Evicted stale entry on read", "file", safeID+".png")
		} else {
			return localPath, nil
		}
	}

	// Validate URL before downloading to prevent SSRF
	parsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "https" {
		return "", fmt.Errorf("insecure protocol: %s", parsedURL.Scheme)
	}

	allowedHosts := map[string]bool{
		"images.pokemontcg.io": true,
		"example.com":          true, // For seed data and testing
	}

	if !allowedHosts[parsedURL.Host] {
		return "", fmt.Errorf("untrusted host: %s", parsedURL.Host)
	}

	// Download for free from remote source
	slog.Info("ImageCache: Downloading card image", "id", cardID, "url", remoteURL)
	resp, err := s.httpClient.Get(remoteURL) // #nosec G107 - internal service downloading card assets
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", os.ErrNotExist
	}

	out, err := os.Create(localPath) // #nosec G304 - path is sanitized with filepath.Base
	if err != nil {
		return "", err
	}
	defer out.Close()

	// Limit downloaded image size to 10MB
	const maxImageSize = 10 << 20
	if resp.ContentLength > maxImageSize {
		return "", fmt.Errorf("image too large: %d bytes (max %d)", resp.ContentLength, maxImageSize)
	}
	limitedReader := &io.LimitedReader{R: resp.Body, N: maxImageSize}

	_, err = io.Copy(out, limitedReader)
	return localPath, err
}
