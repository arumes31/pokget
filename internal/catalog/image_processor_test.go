package catalog

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

type fixedImageHasher struct {
	hash int64
}

func (h fixedImageHasher) CalculateHash(image.Image) (int64, error) {
	return h.hash, nil
}

func TestImageProcessorSupportedFormats(t *testing.T) {
	t.Parallel()

	webp, err := base64.StdEncoding.DecodeString(
		"UklGRh4CAABXRUJQVlA4WAoAAAAgAAAAAQAAAQAASUNDUMgBAAAAAAHIAAAAAAQwAABtbnRyUkdCIFhZWiAH4AAB" +
			"AAEAAAAAAABhY3NwAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQAA9tYAAQAAAADTLQAAAAAAAAAAAAAAAAAA" +
			"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAlkZXNjAAAA8AAAACRyWFlaAAABFAAAABRnWFla" +
			"AAABKAAAABRiWFlaAAABPAAAABR3dHB0AAABUAAAABRyVFJDAAABZAAAAChnVFJDAAABZAAAAChiVFJDAAABZAAA" +
			"AChjcHJ0AAABjAAAADxtbHVjAAAAAAAAAAEAAAAMZW5VUwAAAAgAAAAcAHMAUgBHAEJYWVogAAAAAAAAb6IAADj1" +
			"AAADkFhZWiAAAAAAAABimQAAt4UAABjaWFlaIAAAAAAAACSgAAAPhAAAts9YWVogAAAAAAAA9tYAAQAAAADTLXBh" +
			"cmEAAAAAAAQAAAACZmYAAPKnAAANWQAAE9AAAApbAAAAAAAAAABtbHVjAAAAAAAAAAEAAAAMZW5VUwAAACAAAAAc" +
			"AEcAbwBvAGcAbABlACAASQBuAGMALgAgADIAMAAxADZWUDggMAAAANABAJ0BKgIAAgABQCYloAJ0ugH4AAOwAP7y" +
			"63/82BXNc+/3/9Lg/S4P0uD/0pAAAA==",
	)
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	tests := []struct {
		name      string
		data      []byte
		mimeType  string
		extension string
	}{
		{name: "JPEG", data: encodeJPEG(t, 3, 2), mimeType: "image/jpeg", extension: ".jpg"},
		{name: "PNG", data: encodePNG(t, 3, 2), mimeType: "image/png", extension: ".png"},
		{name: "WebP", data: webp, mimeType: "image/webp", extension: ".webp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/plain")
				writer.Header().Set("ETag", `"image-v1"`)
				writer.Header().Set("Last-Modified", "Tue, 04 Aug 2026 00:00:00 GMT")
				_, _ = writer.Write(test.data)
			}))
			defer server.Close()

			processor := newTestImageProcessor(t, server, ImageProcessorConfig{
				StoreDir: t.TempDir(),
				Hasher:   fixedImageHasher{hash: 42},
			})
			ready, err := processor.Process(context.Background(), ImageJob{
				ID: 1, SourceID: "source", RemoteURL: server.URL + "/card",
			})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if ready.MIMEType != test.mimeType || filepath.Ext(ready.LocalPath) != test.extension {
				t.Fatalf("Process() MIME/path = %q/%q", ready.MIMEType, ready.LocalPath)
			}
			if ready.PHash != 42 || ready.ByteSize != int64(len(test.data)) || ready.ContentSHA256 == "" {
				t.Fatalf("Process() result = %+v", ready)
			}
			if len(ready.Fingerprints) != 3 {
				t.Fatalf("Process() fingerprints = %d, want full and two rotations", len(ready.Fingerprints))
			}
			stored, err := os.ReadFile(ready.LocalPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, test.data) {
				t.Fatal("stored image differs from response")
			}
		})
	}
}

func TestImageProcessorClassifies404AsUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	processor := newTestImageProcessor(t, server, ImageProcessorConfig{StoreDir: t.TempDir()})
	_, err := processor.Process(context.Background(), ImageJob{ID: 1, SourceID: "source", RemoteURL: server.URL})
	if got := ClassifyImageProcessError(err); got != ImageFailureUnavailable {
		t.Fatalf("classification = %q, want %q (error %v)", got, ImageFailureUnavailable, err)
	}
}

func TestImageProcessorRejectsDisallowedRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "https://blocked.invalid/card.png", http.StatusFound)
	}))
	defer server.Close()
	processor := newTestImageProcessor(t, server, ImageProcessorConfig{StoreDir: t.TempDir()})
	_, err := processor.Process(context.Background(), ImageJob{ID: 1, SourceID: "source", RemoteURL: server.URL})
	if got := ClassifyImageProcessError(err); got != ImageFailurePermanent {
		t.Fatalf("classification = %q, want permanent (error %v)", got, err)
	}
}

func TestImageProcessorEnforcesBodyAndPixelLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      []byte
		maxBytes  int64
		maxPixels uint64
	}{
		{name: "body", data: encodePNG(t, 2, 2), maxBytes: 8, maxPixels: 100},
		{name: "pixels", data: encodePNG(t, 11, 11), maxBytes: 1 << 20, maxPixels: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write(test.data)
			}))
			defer server.Close()
			processor := newTestImageProcessor(t, server, ImageProcessorConfig{
				StoreDir:  t.TempDir(),
				MaxBytes:  test.maxBytes,
				MaxPixels: test.maxPixels,
			})
			_, err := processor.Process(context.Background(), ImageJob{ID: 1, SourceID: "source", RemoteURL: server.URL})
			if got := ClassifyImageProcessError(err); got != ImageFailurePermanent {
				t.Fatalf("classification = %q, want permanent (error %v)", got, err)
			}
		})
	}
}

func TestImageProcessorHonorsCancellation(t *testing.T) {
	t.Parallel()

	processor, err := NewImageProcessor(ImageProcessorConfig{
		StoreDir:     t.TempDir(),
		AllowedHosts: map[string][]string{"source": {"images.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = processor.Process(ctx, ImageJob{ID: 1, SourceID: "source", RemoteURL: "https://images.example/card.png"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Process() error = %v, want context.Canceled", err)
	}
}

func newTestImageProcessor(t *testing.T, server *httptest.Server, config ImageProcessorConfig) *ImageProcessor {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config.Client = server.Client()
	config.AllowedHosts = map[string][]string{"source": {serverURL.Hostname()}}
	processor, err := NewImageProcessor(config)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, source); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	source.Set(0, 0, color.RGBA{G: 255, A: 255})
	if err := jpeg.Encode(&output, source, nil); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
