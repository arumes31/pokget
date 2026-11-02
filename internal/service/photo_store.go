package service

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PhotoStoreMaxBytes bounds one uploaded card photo. The scanner sends the JPEG
// it already prepared (max 1800 px edge), which stays well below this limit.
const PhotoStoreMaxBytes int64 = 6 << 20

// photoSegmentPattern accepts the identifiers the store puts into a path: user
// and portfolio ids are UUIDs, staged ids are hex tokens.
var photoSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// PhotoStore keeps the photos collectors take of their own cards. Files live
// outside the database so a large collection does not bloat backups, and the
// path stored in portfolio.photo_path is always relative to Dir.
type PhotoStore struct {
	Dir string
}

// NewPhotoStore prepares the photo directory. A blank directory disables the
// store: scans then simply run without attaching a photo.
func NewPhotoStore(dir string) *PhotoStore {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return &PhotoStore{}
	}
	if err := os.MkdirAll(trimmed, 0700); err != nil { // #nosec G301 - contains private user photos
		return &PhotoStore{}
	}
	return &PhotoStore{Dir: trimmed}
}

// Enabled reports whether photos can be stored.
func (s *PhotoStore) Enabled() bool {
	return s != nil && strings.TrimSpace(s.Dir) != ""
}

// imageExtension validates the image bytes and returns the file extension.
// Only the formats the scanner accepts are stored.
func imageExtension(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", errors.New("photo store: unsupported image format")
	}
}


// Stage writes a scan photo that is not yet attached to a portfolio item. The
// returned token is passed back by the client when the card is saved.
func (s *PhotoStore) Stage(userID, token string, data []byte) (string, error) {
	if !s.Enabled() {
		return "", errors.New("photo store: disabled")
	}
	if !photoSegmentPattern.MatchString(userID) || !photoSegmentPattern.MatchString(token) {
		return "", errors.New("photo store: invalid identifier")
	}
	if len(data) == 0 || int64(len(data)) > PhotoStoreMaxBytes {
		return "", fmt.Errorf("photo store: photo must be between 1 and %d bytes", PhotoStoreMaxBytes)
	}
	extension, err := imageExtension(data)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(s.Dir, "staging", userID)
	if err := os.MkdirAll(directory, 0700); err != nil { // #nosec G301 - private user photos
		return "", err
	}
	relative := filepath.ToSlash(filepath.Join("staging", userID, token+extension))
	path, err := s.Path(relative)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil { // #nosec G306 - private user photos
		return "", err
	}
	s.cleanupStaging(directory)
	return token, nil
}

// findStaged returns the staged photo of one token.
func (s *PhotoStore) findStaged(userID, token string) (string, error) {
	if !photoSegmentPattern.MatchString(userID) || !photoSegmentPattern.MatchString(token) {
		return "", errors.New("photo store: invalid identifier")
	}
	matches, err := filepath.Glob(filepath.Join(s.Dir, "staging", userID, token+".*"))
	if err != nil {
		return "", err
	}
	for _, match := range matches {
		if _, err := imageExtension(mustReadPrefix(match)); err == nil {
			return match, nil
		}
	}
	return "", os.ErrNotExist
}

// mustReadPrefix reads enough bytes to sniff the image type.
func mustReadPrefix(path string) []byte {
	file, err := os.Open(path) // #nosec G304 - caller built the path from validated segments
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil {
		return nil
	}
	return buffer[:read]
}

// Promote turns a staged photo into the permanent photo of a portfolio item.
// The token is the value Stage returned; the staged file always lives in the
// caller's own staging directory, so one user cannot claim another's photo.
func (s *PhotoStore) Promote(userID, token, itemID string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("photo store: disabled")
	}
	if !photoSegmentPattern.MatchString(userID) || !photoSegmentPattern.MatchString(itemID) {
		return "", errors.New("photo store: invalid identifier")
	}
	stagedPath, err := s.findStaged(userID, token)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(stagedPath) // #nosec G304 - path built from validated segments
	if err != nil {
		return "", err
	}
	extension, err := imageExtension(data)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(s.Dir, "items", userID)
	if err := os.MkdirAll(directory, 0700); err != nil { // #nosec G301 - private user photos
		return "", err
	}
	relative := filepath.ToSlash(filepath.Join("items", userID, itemID+extension))
	path, err := s.Path(relative)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil { // #nosec G306 - private user photos
		return "", err
	}
	// The staged copy is only a cache; a failure to delete it is not fatal.
	_ = os.Remove(stagedPath)
	return relative, nil
}

// Path resolves a stored relative path, refusing anything outside Dir.
func (s *PhotoStore) Path(relative string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("photo store: disabled")
	}
	cleaned := filepath.Clean(strings.TrimSpace(relative))
	if cleaned == "" || cleaned == "." || filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" ||
		strings.HasPrefix(cleaned, "..") || os.IsPathSeparator(cleaned[0]) ||
		strings.HasPrefix(filepath.ToSlash(cleaned), "/") {
		return "", errors.New("photo store: invalid path")
	}
	root := filepath.Clean(s.Dir)
	full := filepath.Join(root, cleaned)
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", errors.New("photo store: path escapes the photo directory")
	}
	return full, nil
}

// Read returns the stored photo bytes.
func (s *PhotoStore) Read(relative string) ([]byte, error) {
	path, err := s.Path(relative)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path) // #nosec G304 - path validated by Path
}

// Remove deletes a stored photo, ignoring an already missing file.
func (s *PhotoStore) Remove(relative string) error {
	if !s.Enabled() || strings.TrimSpace(relative) == "" {
		return nil
	}
	path, err := s.Path(relative)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// cleanupStaging drops staged photos the client never attached.
func (s *PhotoStore) cleanupStaging(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-2 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(directory, entry.Name()))
	}
}
