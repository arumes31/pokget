package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jpegBytes is a minimal JPEG header so http.DetectContentType classifies it.
func jpegBytes(payload string) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}, []byte(payload)...)
}

func TestPhotoStoreStagesAndPromotesAPhoto(t *testing.T) {
	directory := t.TempDir()
	store := NewPhotoStore(directory)
	if !store.Enabled() {
		t.Fatal("photo store must be enabled for a real directory")
	}

	token, err := store.Stage("user-1", "token-1", jpegBytes("card"))
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if token != "token-1" {
		t.Fatalf("token = %q, want the staged id", token)
	}

	relative, err := store.Promote("user-1", token, "item-1")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if relative != "items/user-1/item-1.jpg" {
		t.Fatalf("relative = %q", relative)
	}
	data, err := store.Read(relative)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Contains(data, []byte("card")) {
		t.Fatalf("stored photo lost its payload: %q", data)
	}

	// The staged copy is consumed by the promotion.
	staged := filepath.Join(directory, "staging", "user-1", "token-1.jpg")
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged photo still exists: %v", err)
	}

	if err := store.Remove(relative); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := store.Read(relative); err == nil {
		t.Fatal("removed photo is still readable")
	}
	// Removing twice stays harmless.
	if err := store.Remove(relative); err != nil {
		t.Fatalf("Remove twice: %v", err)
	}
}

func TestPhotoStoreRejectsUnsupportedAndOversizedInput(t *testing.T) {
	store := NewPhotoStore(t.TempDir())

	if _, err := store.Stage("user-1", "token-1", []byte("not an image at all")); err == nil {
		t.Fatal("expected an error for non-image bytes")
	}
	oversized := make([]byte, PhotoStoreMaxBytes+1)
	copy(oversized, jpegBytes(""))
	if _, err := store.Stage("user-1", "token-2", oversized); err == nil {
		t.Fatal("expected an error for an oversized photo")
	}
	if _, err := store.Stage("", "token-3", jpegBytes("x")); err == nil {
		t.Fatal("expected an error for a missing user id")
	}
	if _, err := store.Stage("../escape", "token-4", jpegBytes("x")); err == nil {
		t.Fatal("expected an error for an unsafe user id")
	}
}

func TestPhotoStoreRefusesAnotherUsersStagedPhoto(t *testing.T) {
	directory := t.TempDir()
	store := NewPhotoStore(directory)
	if _, err := store.Stage("user-1", "token-1", jpegBytes("card")); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if _, err := store.Promote("user-2", "token-1", "item-1"); err == nil {
		t.Fatal("expected an error when promoting another user's staged photo")
	}
}

func TestPhotoStorePathStaysInsideTheDirectory(t *testing.T) {
	store := NewPhotoStore(t.TempDir())
	for _, candidate := range []string{"", ".", "..", "../etc/passwd", "items/../../escape.jpg", "/abs/path.jpg"} {
		if _, err := store.Path(candidate); err == nil {
			t.Fatalf("Path(%q) must be rejected", candidate)
		}
	}
	if _, err := store.Path("items/user-1/item-1.jpg"); err != nil {
		t.Fatalf("Path: %v", err)
	}
}

func TestPhotoStoreDisabledWithoutADirectory(t *testing.T) {
	store := NewPhotoStore("  ")
	if store.Enabled() {
		t.Fatal("an empty directory must disable the store")
	}
	if _, err := store.Stage("user-1", "token-1", jpegBytes("x")); err == nil {
		t.Fatal("expected an error from a disabled store")
	}
	if _, err := (*PhotoStore)(nil).Read("items/x.jpg"); err == nil {
		t.Fatal("a nil store must not read")
	}
	if strings.TrimSpace(store.Dir) != "" {
		t.Fatalf("disabled store has a directory: %q", store.Dir)
	}
}
