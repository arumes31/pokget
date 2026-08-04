package detectiontest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	defaultMaxBytes  = int64(8 << 20)
	defaultMaxPixels = int64(40_000_000)
	defaultTimeout   = 30 * time.Second
	defaultUserAgent = "pokget-detection-fixtures/1.0"
)

// Downloader fetches and validates immutable source images.
type Downloader struct {
	client    *http.Client
	maxBytes  int64
	maxPixels int64
	userAgent string
}

// DownloaderConfig configures bounded source-image downloads.
type DownloaderConfig struct {
	Client    *http.Client
	MaxBytes  int64
	MaxPixels int64
	UserAgent string
}

// FetchedImage is an integrity-checked source image.
type FetchedImage struct {
	Bytes  []byte
	Image  image.Image
	Format string
	MIME   string
}

// NewDownloader constructs a downloader with bounded production defaults.
func NewDownloader(config DownloaderConfig) (*Downloader, error) {
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	if client.Timeout <= 0 {
		return nil, errors.New("fixture downloader: http client timeout must be positive")
	}

	maxBytes := config.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes < 1 {
		return nil, errors.New("fixture downloader: max bytes must be positive")
	}

	maxPixels := config.MaxPixels
	if maxPixels == 0 {
		maxPixels = defaultMaxPixels
	}
	if maxPixels < 1 {
		return nil, errors.New("fixture downloader: max pixels must be positive")
	}

	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	clientCopy := *client
	originalRedirectCheck := client.CheckRedirect
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" {
			return errors.New("fixture downloader: redirect left https")
		}
		if originalRedirectCheck != nil {
			return originalRedirectCheck(request, via)
		}
		if len(via) >= 10 {
			return errors.New("fixture downloader: stopped after 10 redirects")
		}
		return nil
	}

	return &Downloader{
		client:    &clientCopy,
		maxBytes:  maxBytes,
		maxPixels: maxPixels,
		userAgent: userAgent,
	}, nil
}

// Fetch downloads one card image and enforces the manifest integrity contract.
func (d *Downloader) Fetch(ctx context.Context, card Card) (FetchedImage, error) {
	if d == nil || d.client == nil {
		return FetchedImage{}, errors.New("fetching fixture: nil downloader")
	}
	if err := validateHTTPSURL(card.ImageURL); err != nil {
		return FetchedImage{}, fmt.Errorf("fetching fixture %q: %w", card.SourceID, err)
	}
	if card.ImageSize > d.maxBytes {
		return FetchedImage{}, fmt.Errorf(
			"fetching fixture %q: expected size %d exceeds limit %d",
			card.SourceID,
			card.ImageSize,
			d.maxBytes,
		)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, card.ImageURL, nil)
	if err != nil {
		return FetchedImage{}, fmt.Errorf("creating fixture request %q: %w", card.SourceID, err)
	}
	request.Header.Set("Accept", card.ImageMIME)
	request.Header.Set("User-Agent", d.userAgent)

	response, err := d.client.Do(request)
	if err != nil {
		return FetchedImage{}, fmt.Errorf("downloading fixture %q: %w", card.SourceID, err)
	}
	defer response.Body.Close()

	if response.Request.URL.Scheme != "https" {
		return FetchedImage{}, fmt.Errorf("downloading fixture %q: redirect left https", card.SourceID)
	}
	if response.StatusCode != http.StatusOK {
		return FetchedImage{}, fmt.Errorf(
			"downloading fixture %q: unexpected status %s",
			card.SourceID,
			response.Status,
		)
	}
	if response.ContentLength > d.maxBytes {
		return FetchedImage{}, fmt.Errorf(
			"downloading fixture %q: content length %d exceeds limit %d",
			card.SourceID,
			response.ContentLength,
			d.maxBytes,
		)
	}

	responseMIME, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return FetchedImage{}, fmt.Errorf("downloading fixture %q: parsing content type: %w", card.SourceID, err)
	}
	if responseMIME != card.ImageMIME {
		return FetchedImage{}, fmt.Errorf(
			"downloading fixture %q: got content type %q, want %q",
			card.SourceID,
			responseMIME,
			card.ImageMIME,
		)
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, d.maxBytes+1))
	if err != nil {
		return FetchedImage{}, fmt.Errorf("reading fixture %q: %w", card.SourceID, err)
	}
	if int64(len(payload)) > d.maxBytes {
		return FetchedImage{}, fmt.Errorf("reading fixture %q: response exceeds limit %d", card.SourceID, d.maxBytes)
	}
	if int64(len(payload)) != card.ImageSize {
		return FetchedImage{}, fmt.Errorf(
			"validating fixture %q: got size %d, want %d",
			card.SourceID,
			len(payload),
			card.ImageSize,
		)
	}

	sniffedMIME := http.DetectContentType(payload)
	if sniffedMIME != card.ImageMIME {
		return FetchedImage{}, fmt.Errorf(
			"validating fixture %q: bytes are %q, want %q",
			card.SourceID,
			sniffedMIME,
			card.ImageMIME,
		)
	}

	expectedHash, err := hex.DecodeString(card.ImageSHA256)
	if err != nil || len(expectedHash) != sha256.Size {
		return FetchedImage{}, fmt.Errorf("validating fixture %q: invalid expected sha256", card.SourceID)
	}
	actualHash := sha256.Sum256(payload)
	if subtle.ConstantTimeCompare(actualHash[:], expectedHash) != 1 {
		return FetchedImage{}, fmt.Errorf(
			"validating fixture %q: got sha256 %s, want %s",
			card.SourceID,
			hex.EncodeToString(actualHash[:]),
			card.ImageSHA256,
		)
	}

	config, configFormat, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return FetchedImage{}, fmt.Errorf("decoding fixture config %q: %w", card.SourceID, err)
	}
	if configFormat != card.ImageFormat {
		return FetchedImage{}, fmt.Errorf(
			"decoding fixture %q: got format %q, want %q",
			card.SourceID,
			configFormat,
			card.ImageFormat,
		)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return FetchedImage{}, fmt.Errorf("decoding fixture %q: non-positive dimensions", card.SourceID)
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels > d.maxPixels {
		return FetchedImage{}, fmt.Errorf(
			"decoding fixture %q: %d pixels exceeds limit %d",
			card.SourceID,
			pixels,
			d.maxPixels,
		)
	}

	decoded, format, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return FetchedImage{}, fmt.Errorf("decoding fixture %q: %w", card.SourceID, err)
	}
	if format != card.ImageFormat {
		return FetchedImage{}, fmt.Errorf(
			"decoding fixture %q: got format %q, want %q",
			card.SourceID,
			format,
			card.ImageFormat,
		)
	}
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return FetchedImage{}, fmt.Errorf("decoding fixture %q: dimensions changed during decode", card.SourceID)
	}

	return FetchedImage{
		Bytes:  payload,
		Image:  decoded,
		Format: format,
		MIME:   responseMIME,
	}, nil
}
