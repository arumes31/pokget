package detectiontest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownloaderFetch(t *testing.T) {
	t.Parallel()

	payload := testPNG(t, 12, 18)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "fixture-test/1.0" {
			t.Errorf("User-Agent = %q, want fixture-test/1.0", request.Header.Get("User-Agent"))
		}
		if request.Header.Get("Accept") != "image/png" {
			t.Errorf("Accept = %q, want image/png", request.Header.Get("Accept"))
		}
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = time.Second
	downloader, err := NewDownloader(DownloaderConfig{
		Client:    client,
		UserAgent: "fixture-test/1.0",
	})
	if err != nil {
		t.Fatalf("NewDownloader() error = %v", err)
	}
	card := downloadTestCard(server.URL, payload)

	fetched, err := downloader.Fetch(t.Context(), card)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !bytes.Equal(fetched.Bytes, payload) {
		t.Error("Fetch() returned different bytes")
	}
	if fetched.Format != "png" || fetched.MIME != "image/png" {
		t.Errorf("Fetch() format/mime = %q/%q, want png/image/png", fetched.Format, fetched.MIME)
	}
	if fetched.Image.Bounds().Dx() != 12 || fetched.Image.Bounds().Dy() != 18 {
		t.Errorf("Fetch() dimensions = %v, want 12x18", fetched.Image.Bounds())
	}
}

func TestDownloaderFetchRejectsInvalidResponse(t *testing.T) {
	t.Parallel()

	validPayload := testPNG(t, 12, 18)
	invalidPNG := []byte("\x89PNG\r\n\x1a\ninvalid")
	tests := []struct {
		name        string
		status      int
		contentType string
		payload     []byte
		mutate      func(*Card)
		expected    string
	}{
		{
			name:        "status",
			status:      http.StatusBadGateway,
			contentType: "image/png",
			payload:     validPayload,
			expected:    "unexpected status",
		},
		{
			name:        "mime",
			status:      http.StatusOK,
			contentType: "text/html",
			payload:     validPayload,
			expected:    "content type",
		},
		{
			name:        "size",
			status:      http.StatusOK,
			contentType: "image/png",
			payload:     validPayload,
			mutate: func(card *Card) {
				card.ImageSize++
			},
			expected: "got size",
		},
		{
			name:        "hash",
			status:      http.StatusOK,
			contentType: "image/png",
			payload:     validPayload,
			mutate: func(card *Card) {
				card.ImageSHA256 = strings.Repeat("0", 64)
			},
			expected: "got sha256",
		},
		{
			name:        "decode",
			status:      http.StatusOK,
			contentType: "image/png",
			payload:     invalidPNG,
			expected:    "decoding fixture config",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", test.contentType)
				response.WriteHeader(test.status)
				_, _ = response.Write(test.payload)
			}))
			defer server.Close()

			client := server.Client()
			client.Timeout = time.Second
			downloader, err := NewDownloader(DownloaderConfig{Client: client})
			if err != nil {
				t.Fatalf("NewDownloader() error = %v", err)
			}
			card := downloadTestCard(server.URL, test.payload)
			if test.mutate != nil {
				test.mutate(&card)
			}
			_, err = downloader.Fetch(t.Context(), card)
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Errorf("Fetch() error = %v, want containing %q", err, test.expected)
			}
		})
	}
}

func TestDownloaderFetchRejectsInsecureRedirect(t *testing.T) {
	t.Parallel()

	payload := testPNG(t, 12, 18)
	insecureServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write(payload)
	}))
	defer insecureServer.Close()

	secureServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, insecureServer.URL, http.StatusFound)
	}))
	defer secureServer.Close()

	client := secureServer.Client()
	client.Timeout = time.Second
	downloader, err := NewDownloader(DownloaderConfig{Client: client})
	if err != nil {
		t.Fatalf("NewDownloader() error = %v", err)
	}
	_, err = downloader.Fetch(t.Context(), downloadTestCard(secureServer.URL, payload))
	if err == nil || !strings.Contains(err.Error(), "redirect left https") {
		t.Errorf("Fetch() error = %v, want insecure redirect error", err)
	}
}

func testPNG(t *testing.T, width int, height int) []byte {
	t.Helper()

	fixture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			fixture.SetNRGBA(x, y, color.NRGBA{
				R: testColorChannel(x * 11),
				G: testColorChannel(y * 7),
				B: testColorChannel((x + y) * 5),
				A: 255,
			})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, fixture); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return output.Bytes()
}

func downloadTestCard(imageURL string, payload []byte) Card {
	digest := sha256.Sum256(payload)
	return Card{
		SourceID:    "test-card",
		ImageURL:    imageURL,
		ImageSHA256: hex.EncodeToString(digest[:]),
		ImageSize:   int64(len(payload)),
		ImageMIME:   "image/png",
		ImageFormat: "png",
	}
}
