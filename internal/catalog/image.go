package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	FingerprintAlgorithm        = "phash64"
	FingerprintAlgorithmVersion = int16(1)
	FingerprintTransform        = "full"
)

var ErrImageLeaseLost = errors.New("catalog: image lease lost")

type ImageJob struct {
	ID        int64
	CardID    string
	SourceID  string
	RemoteURL string
	Attempts  int
}

type ReadyImage struct {
	ImageID            int64
	LeaseOwner         string
	LocalPath          string
	ContentSHA256      string
	RemoteETag         string
	RemoteLastModified string
	MIMEType           string
	Width              int
	Height             int
	ByteSize           int64
	PHash              int64
	Fingerprints       []ImageFingerprint
}

type ImageFingerprint struct {
	Algorithm        string
	AlgorithmVersion int16
	Transform        string
	Hash             int64
}

type ImageFailureKind string

const (
	ImageFailureUnavailable ImageFailureKind = "unavailable"
	ImageFailureRetryable   ImageFailureKind = "retryable"
	ImageFailurePermanent   ImageFailureKind = "permanent"
)

func (k ImageFailureKind) Valid() bool {
	switch k {
	case ImageFailureUnavailable, ImageFailureRetryable, ImageFailurePermanent:
		return true
	default:
		return false
	}
}

type ImageFailure struct {
	ImageID    int64
	LeaseOwner string
	Kind       ImageFailureKind
	RetryAt    *time.Time
	Cause      error
}

type ImageQueue interface {
	LeaseImageJobs(context.Context, string, int, time.Duration) ([]ImageJob, error)
	MarkImageReady(context.Context, ReadyImage) error
	MarkImageFailed(context.Context, ImageFailure) error
}

type ImageProcessError struct {
	Kind ImageFailureKind
	Err  error
}

func (e *ImageProcessError) Error() string {
	if e == nil || e.Err == nil {
		return "catalog: image processing failed"
	}
	return e.Err.Error()
}

func (e *ImageProcessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewImageProcessError(kind ImageFailureKind, err error) error {
	if err == nil {
		err = errors.New("catalog: image processing failed")
	}
	if !kind.Valid() {
		kind = ImageFailurePermanent
	}
	return &ImageProcessError{Kind: kind, Err: err}
}

func ClassifyImageProcessError(err error) ImageFailureKind {
	var processError *ImageProcessError
	if errors.As(err, &processError) && processError.Kind.Valid() {
		return processError.Kind
	}
	return ImageFailureRetryable
}

func RetryDelay(attempt int, base, maximum time.Duration) time.Duration {
	if base <= 0 || maximum <= 0 {
		return 0
	}
	if base >= maximum {
		return maximum
	}
	if attempt < 1 {
		attempt = 1
	}

	delay := base
	for current := 1; current < attempt; current++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func validateImageFailure(failure ImageFailure) error {
	switch {
	case failure.ImageID <= 0:
		return fmt.Errorf("catalog: image id must be positive")
	case failure.LeaseOwner == "":
		return fmt.Errorf("catalog: image lease owner is required")
	case !failure.Kind.Valid():
		return fmt.Errorf("catalog: invalid image failure kind %q", failure.Kind)
	case failure.Cause == nil:
		return fmt.Errorf("catalog: image failure cause is required")
	case failure.Kind == ImageFailureRetryable && failure.RetryAt == nil:
		return fmt.Errorf("catalog: retryable image failure requires a retry time")
	case failure.Kind != ImageFailureRetryable && failure.RetryAt != nil:
		return fmt.Errorf("catalog: non-retryable image failure cannot have a retry time")
	default:
		return nil
	}
}
