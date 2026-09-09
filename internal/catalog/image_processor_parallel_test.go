package catalog

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestImageProcessorParallelPreservesContentAndFingerprints(t *testing.T) {
	t.Parallel()
	data := encodeJPEG(t, 180, 250)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	baseline := newTestImageProcessor(t, server, ImageProcessorConfig{StoreDir: t.TempDir()})
	job := ImageJob{ID: 1, SourceID: "source", RemoteURL: server.URL}
	want, err := baseline.Process(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	processor := newTestImageProcessor(t, server, ImageProcessorConfig{StoreDir: t.TempDir()})
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			got, err := processor.Process(t.Context(), job)
			if err != nil {
				t.Error(err)
				return
			}
			if got.PHash != want.PHash || got.ContentSHA256 != want.ContentSHA256 || !reflect.DeepEqual(got.Fingerprints, want.Fingerprints) {
				t.Error("parallel processing changed image fingerprints")
			}
			stored, err := os.ReadFile(got.LocalPath)
			if err != nil || !bytes.Equal(stored, data) {
				t.Errorf("stored content differs: %v", err)
			}
		})
	}
	group.Wait()
}

func TestImageProcessorLogsStagesWithoutURLs(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	data := encodePNG(t, 3, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	processor := newTestImageProcessor(t, server, ImageProcessorConfig{StoreDir: t.TempDir()})
	_, err := processor.Process(t.Context(), ImageJob{ID: 42, SourceID: "source", RemoteURL: server.URL + "?secret=not-for-logs"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"image_id=42", "stage=download", "stage=decode", "stage=hash", "stage=store", "outcome=ready", "download_ms=", "decode_ms=", "hash_ms=", "store_ms="} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("missing log field %q", field)
		}
	}
	if strings.Contains(logs.String(), "not-for-logs") || strings.Contains(logs.String(), server.URL) {
		t.Fatal("image URL leaked into diagnostics")
	}
}
