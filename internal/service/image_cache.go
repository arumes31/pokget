package service

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

type ImageCacheService struct {
	BaseDir string
}

func NewImageCacheService(baseDir string) *ImageCacheService {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0700); err != nil { // #nosec G301 - restricted permissions
		slog.Error("Failed to create image cache directory", "dir", baseDir, "error", err)
	}
	return &ImageCacheService{BaseDir: baseDir}
}

// GetImagePath returns the local path for a card image, downloading it if necessary
func (s *ImageCacheService) GetImagePath(cardID string, remoteURL string) (string, error) {
	// Sanitize cardID to prevent directory traversal
	safeID := filepath.Base(cardID)
	localPath := filepath.Join(s.BaseDir, safeID+".png")

	// Check if already exists
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// Download for free from remote source
	slog.Info("ImageCache: Downloading card image", "id", cardID, "url", remoteURL)
	resp, err := http.Get(remoteURL) // #nosec G107 - internal service downloading card assets
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

	_, err = io.Copy(out, resp.Body)
	return localPath, err
}
