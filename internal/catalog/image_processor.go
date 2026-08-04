package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/corona10/goimagehash"
	_ "golang.org/x/image/webp"
)

const (
	DefaultImageMaxBytes  int64  = 12 << 20
	DefaultImageMaxPixels uint64 = 40_000_000
	defaultImageTimeout          = 30 * time.Second
	defaultRedirectLimit         = 10
)

type ImageHasher interface {
	CalculateHash(image.Image) (int64, error)
}

type PerceptualImageHasher struct{}

func (PerceptualImageHasher) CalculateHash(source image.Image) (int64, error) {
	if source == nil {
		return 0, fmt.Errorf("catalog: cannot hash a nil image")
	}
	hash, err := goimagehash.PerceptionHash(source)
	if err != nil {
		return 0, fmt.Errorf("catalog: calculating image pHash: %w", err)
	}
	return int64(hash.GetHash()), nil // #nosec G115 -- the database stores the 64 raw bits in BIGINT.
}

type ImageProcessorConfig struct {
	StoreDir     string
	Client       *http.Client
	AllowedHosts map[string][]string
	Hasher       ImageHasher
	MaxBytes     int64
	MaxPixels    uint64
}

type ImageProcessor struct {
	storeDir     string
	client       *http.Client
	allowedHosts map[string]map[string]struct{}
	hasher       ImageHasher
	maxBytes     int64
	maxPixels    uint64
}

func NewImageProcessor(config ImageProcessorConfig) (*ImageProcessor, error) {
	if strings.TrimSpace(config.StoreDir) == "" {
		return nil, fmt.Errorf("catalog: image store directory is required")
	}
	storeDir, err := filepath.Abs(config.StoreDir)
	if err != nil {
		return nil, fmt.Errorf("catalog: resolving image store directory: %w", err)
	}
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil, fmt.Errorf("catalog: creating image store directory: %w", err)
	}

	allowedHosts, err := normalizeAllowedHosts(config.AllowedHosts)
	if err != nil {
		return nil, err
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultImageTimeout}
	}
	hasher := config.Hasher
	if hasher == nil {
		hasher = PerceptualImageHasher{}
	}
	maxBytes := config.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultImageMaxBytes
	}
	maxPixels := config.MaxPixels
	if maxPixels == 0 {
		maxPixels = DefaultImageMaxPixels
	}

	return &ImageProcessor{
		storeDir:     storeDir,
		client:       client,
		allowedHosts: allowedHosts,
		hasher:       hasher,
		maxBytes:     maxBytes,
		maxPixels:    maxPixels,
	}, nil
}

func (p *ImageProcessor) Process(ctx context.Context, job ImageJob) (ReadyImage, error) {
	if err := ctx.Err(); err != nil {
		return ReadyImage{}, err
	}
	if job.ID <= 0 {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: image id must be positive"))
	}
	remoteURL, err := url.Parse(job.RemoteURL)
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: parsing image URL: %w", err))
	}
	if err := p.validateURL(job.SourceID, remoteURL); err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL.String(), nil)
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: creating image request: %w", err))
	}
	request.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	request.Header.Set("User-Agent", "pokget-catalog-image-worker/1")

	client := *p.client
	previousRedirectCheck := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= defaultRedirectLimit {
			return NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: too many image redirects"))
		}
		if err := p.validateURL(job.SourceID, request.URL); err != nil {
			return NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: rejecting image redirect: %w", err))
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(request, via)
		}
		return nil
	}

	response, err := client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ReadyImage{}, ctxErr
		}
		var processError *ImageProcessError
		if errors.As(err, &processError) {
			return ReadyImage{}, processError
		}
		return ReadyImage{}, NewImageProcessError(ImageFailureRetryable, fmt.Errorf("catalog: downloading image: %w", err))
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return ReadyImage{}, NewImageProcessError(ImageFailureUnavailable, fmt.Errorf("catalog: image returned %s", response.Status))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return ReadyImage{}, NewImageProcessError(ImageFailureRetryable, fmt.Errorf("catalog: image returned %s", response.Status))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: image returned %s", response.Status))
	}
	if response.ContentLength > p.maxBytes {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: image content length %d exceeds %d bytes", response.ContentLength, p.maxBytes))
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, p.maxBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ReadyImage{}, ctxErr
		}
		return ReadyImage{}, NewImageProcessError(ImageFailureRetryable, fmt.Errorf("catalog: reading image body: %w", err))
	}
	if int64(len(data)) > p.maxBytes {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: image exceeds %d bytes", p.maxBytes))
	}
	if err := ctx.Err(); err != nil {
		return ReadyImage{}, err
	}

	mimeType, extension, err := sniffImageType(data)
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, err)
	}
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: decoding image dimensions: %w", err))
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 ||
		uint64(imageConfig.Width)*uint64(imageConfig.Height) > p.maxPixels {
		return ReadyImage{}, NewImageProcessError(
			ImageFailurePermanent,
			fmt.Errorf("catalog: decoded image dimensions %dx%d exceed %d pixels", imageConfig.Width, imageConfig.Height, p.maxPixels),
		)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, fmt.Errorf("catalog: decoding image: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return ReadyImage{}, err
	}
	phash, err := p.hasher.CalculateHash(decoded)
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailurePermanent, err)
	}

	contentHash := sha256.Sum256(data)
	contentSHA256 := hex.EncodeToString(contentHash[:])
	localPath, err := p.store(contentSHA256, extension, data)
	if err != nil {
		return ReadyImage{}, NewImageProcessError(ImageFailureRetryable, err)
	}

	return ReadyImage{
		ImageID:            job.ID,
		LocalPath:          localPath,
		ContentSHA256:      contentSHA256,
		RemoteETag:         response.Header.Get("ETag"),
		RemoteLastModified: response.Header.Get("Last-Modified"),
		MIMEType:           mimeType,
		Width:              imageConfig.Width,
		Height:             imageConfig.Height,
		ByteSize:           int64(len(data)),
		PHash:              phash,
		Fingerprints:       p.transformedFingerprints(decoded, phash),
	}, nil
}

func (p *ImageProcessor) transformedFingerprints(source image.Image, fullHash int64) []ImageFingerprint {
	result := []ImageFingerprint{{
		Algorithm: FingerprintAlgorithm, AlgorithmVersion: FingerprintAlgorithmVersion,
		Transform: FingerprintTransform, Hash: fullHash,
	}}
	for _, angle := range []float64{-3, 3} {
		rotated := rotateImage(source, angle)
		hash, err := p.hasher.CalculateHash(rotated)
		if err != nil {
			continue
		}
		result = append(result, ImageFingerprint{
			Algorithm: FingerprintAlgorithm, AlgorithmVersion: FingerprintAlgorithmVersion,
			Transform: fmt.Sprintf("rotate_%+.0f", angle), Hash: hash,
		})
	}
	return result
}

func rotateImage(source image.Image, degrees float64) *image.NRGBA {
	bounds := source.Bounds()
	destination := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(destination, destination.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	radians := degrees * math.Pi / 180
	sine, cosine := math.Sincos(radians)
	centerX := float64(bounds.Dx()-1) / 2
	centerY := float64(bounds.Dy()-1) / 2
	for y := 0; y < destination.Bounds().Dy(); y++ {
		for x := 0; x < destination.Bounds().Dx(); x++ {
			destinationX := float64(x) - centerX
			destinationY := float64(y) - centerY
			sourceX := cosine*destinationX + sine*destinationY + centerX
			sourceY := -sine*destinationX + cosine*destinationY + centerY
			nearestX := int(math.Round(sourceX)) + bounds.Min.X
			nearestY := int(math.Round(sourceY)) + bounds.Min.Y
			if nearestX >= bounds.Min.X && nearestX < bounds.Max.X && nearestY >= bounds.Min.Y && nearestY < bounds.Max.Y {
				destination.Set(x, y, source.At(nearestX, nearestY))
			}
		}
	}
	return destination
}

func (p *ImageProcessor) validateURL(sourceID string, target *url.URL) error {
	if target == nil || !strings.EqualFold(target.Scheme, "https") {
		return fmt.Errorf("catalog: image URL must use HTTPS")
	}
	if target.User != nil {
		return fmt.Errorf("catalog: image URL cannot contain user information")
	}
	host := normalizeHostname(target.Hostname())
	if host == "" {
		return fmt.Errorf("catalog: image URL hostname is required")
	}
	hosts, exists := p.allowedHosts[sourceID]
	if !exists {
		return fmt.Errorf("catalog: no image host allowlist configured for source %q", sourceID)
	}
	if _, allowed := hosts[host]; !allowed {
		return fmt.Errorf("catalog: image hostname %q is not allowed for source %q", host, sourceID)
	}
	return nil
}

func (p *ImageProcessor) store(contentHash, extension string, data []byte) (string, error) {
	shard := contentHash[:2]
	directory := filepath.Join(p.storeDir, shard)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("catalog: creating content-addressed image directory: %w", err)
	}
	localPath := filepath.Join(directory, contentHash+extension)
	if info, err := os.Stat(localPath); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("catalog: content-addressed image path is not a regular file")
		}
		return localPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("catalog: checking content-addressed image: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".image-*")
	if err != nil {
		return "", fmt.Errorf("catalog: creating temporary image: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("catalog: securing temporary image: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("catalog: writing temporary image: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("catalog: syncing temporary image: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("catalog: closing temporary image: %w", err)
	}
	if err := os.Rename(temporaryPath, localPath); err != nil {
		if info, statErr := os.Stat(localPath); statErr == nil && info.Mode().IsRegular() {
			return localPath, nil
		}
		return "", fmt.Errorf("catalog: publishing content-addressed image: %w", err)
	}
	removeTemporary = false
	return localPath, nil
}

func normalizeAllowedHosts(configured map[string][]string) (map[string]map[string]struct{}, error) {
	if len(configured) == 0 {
		return nil, fmt.Errorf("catalog: per-source image host allowlist is required")
	}
	result := make(map[string]map[string]struct{}, len(configured))
	for sourceID, hosts := range configured {
		if strings.TrimSpace(sourceID) == "" || len(hosts) == 0 {
			return nil, fmt.Errorf("catalog: image host allowlist contains an empty source or host list")
		}
		result[sourceID] = make(map[string]struct{}, len(hosts))
		for _, host := range hosts {
			normalized := normalizeHostname(host)
			if normalized == "" || strings.Contains(normalized, ":") || strings.ContainsAny(normalized, "/?#@") {
				return nil, fmt.Errorf("catalog: invalid image hostname %q for source %q", host, sourceID)
			}
			result[sourceID][normalized] = struct{}{}
		}
	}
	return result, nil
}

func normalizeHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func sniffImageType(data []byte) (string, string, error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("catalog: image body is empty")
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	switch mimeType := http.DetectContentType(sample); mimeType {
	case "image/jpeg":
		return mimeType, ".jpg", nil
	case "image/png":
		return mimeType, ".png", nil
	case "image/webp":
		return mimeType, ".webp", nil
	default:
		return "", "", fmt.Errorf("catalog: unsupported image MIME type %q", mimeType)
	}
}
