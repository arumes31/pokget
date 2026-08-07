package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const maxImageLeaseBatch = 1000

func (r *PostgresRepository) LeaseImageJobs(
	ctx context.Context,
	owner string,
	limit int,
	leaseDuration time.Duration,
) ([]ImageJob, error) {
	return r.leaseImageJobs(ctx, owner, "", limit, leaseDuration)
}

// LeaseImageJobsForCard leases image work for one canonical card. It is used
// by explicit verification and on-demand processing without changing the
// ordering of the background queue.
func (r *PostgresRepository) LeaseImageJobsForCard(
	ctx context.Context,
	owner string,
	cardID string,
	limit int,
	leaseDuration time.Duration,
) ([]ImageJob, error) {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return nil, fmt.Errorf("catalog: card id is required for a targeted image lease")
	}
	return r.leaseImageJobs(ctx, owner, cardID, limit, leaseDuration)
}

func (r *PostgresRepository) ImageProgress(ctx context.Context) (ImageProgress, error) {
	var progress ImageProgress
	err := r.db.QueryRowContext(ctx, `
		WITH eligible_images AS MATERIALIZED (
			SELECT image.id, image.status, image.next_attempt_at
			FROM card_images AS image
			JOIN cards AS card ON card.id = image.card_id
			JOIN catalog_sources AS source ON source.id = card.source_id
			WHERE card.catalog_active = TRUE
			  AND source.enabled = TRUE
		), image_counts AS (
			SELECT COUNT(*) AS total,
			       COUNT(*) FILTER (WHERE status = 'ready') AS ready,
			       COUNT(*) FILTER (WHERE status = 'pending') AS pending,
			       COUNT(*) FILTER (WHERE status = 'processing') AS processing,
			       COUNT(*) FILTER (WHERE status = 'failed' AND next_attempt_at IS NOT NULL) AS retryable,
			       COUNT(*) FILTER (WHERE status = 'failed' AND next_attempt_at IS NULL) AS failed,
			       COUNT(*) FILTER (WHERE status = 'unavailable') AS unavailable
			FROM eligible_images
		)
		SELECT image_counts.total, image_counts.ready, image_counts.pending,
		       image_counts.processing, image_counts.retryable, image_counts.failed,
		       image_counts.unavailable,
		       (SELECT COUNT(*)
		        FROM card_fingerprints AS fingerprint
		        JOIN eligible_images AS image ON image.id = fingerprint.image_id
		        WHERE fingerprint.algorithm = $1 AND fingerprint.algorithm_version = $2)
		FROM image_counts`, FingerprintAlgorithm, FingerprintAlgorithmVersion).Scan(
		&progress.Total,
		&progress.Ready,
		&progress.Pending,
		&progress.Processing,
		&progress.Retryable,
		&progress.Failed,
		&progress.Unavailable,
		&progress.Fingerprints,
	)
	if err != nil {
		return ImageProgress{}, fmt.Errorf("catalog: loading image fingerprint progress: %w", err)
	}
	return progress, nil
}

func (r *PostgresRepository) leaseImageJobs(
	ctx context.Context,
	owner string,
	cardID string,
	limit int,
	leaseDuration time.Duration,
) ([]ImageJob, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("catalog: image lease owner is required")
	}
	if limit <= 0 || limit > maxImageLeaseBatch {
		return nil, fmt.Errorf("catalog: image lease limit must be between 1 and %d", maxImageLeaseBatch)
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("catalog: image lease duration must be positive")
	}
	leaseSeconds := int64(math.Ceil(leaseDuration.Seconds()))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: beginning image lease transaction: %w", err)
	}
	rows, err := tx.QueryContext(
		ctx,
		`WITH candidates AS (
		     SELECT image.id
		     FROM card_images AS image
		     JOIN cards AS card ON card.id = image.card_id
		     JOIN catalog_sources AS source ON source.id = card.source_id
		     WHERE card.catalog_active = TRUE
		       AND source.enabled = TRUE
		       AND ($4 = '' OR image.card_id = $4)
		       AND (
		         image.status = 'pending'
		         OR (image.status = 'failed' AND image.next_attempt_at <= NOW())
		         OR (image.status = 'processing' AND image.lease_until < NOW())
		       )
		     ORDER BY COALESCE(image.next_attempt_at, image.first_seen_at), image.id
		     FOR UPDATE OF image SKIP LOCKED
		     LIMIT $1
		 )
		 UPDATE card_images AS image
		 SET status = 'processing', attempts = image.attempts + 1,
		     lease_owner = $2, lease_until = NOW() + ($3 * INTERVAL '1 second'),
		     next_attempt_at = NULL, last_error = NULL
		 FROM candidates, cards AS card
		 WHERE image.id = candidates.id AND card.id = image.card_id
		 RETURNING image.id, image.card_id, card.source_id, image.remote_url, image.attempts`,
		limit,
		owner,
		leaseSeconds,
		cardID,
	)
	if err != nil {
		return nil, rollback(tx, fmt.Errorf("catalog: leasing image jobs: %w", err))
	}

	jobs := make([]ImageJob, 0, limit)
	for rows.Next() {
		var job ImageJob
		if err := rows.Scan(&job.ID, &job.CardID, &job.SourceID, &job.RemoteURL, &job.Attempts); err != nil {
			_ = rows.Close()
			return nil, rollback(tx, fmt.Errorf("catalog: scanning leased image job: %w", err))
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, rollback(tx, fmt.Errorf("catalog: reading leased image jobs: %w", err))
	}
	if err := rows.Close(); err != nil {
		return nil, rollback(tx, fmt.Errorf("catalog: closing leased image jobs: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("catalog: committing image leases: %w", err)
	}
	return jobs, nil
}

func (r *PostgresRepository) MarkImageReady(ctx context.Context, ready ReadyImage) error {
	if err := validateReadyImage(ready); err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: beginning ready image transaction: %w", err)
	}
	cardID, err := lockImageLease(ctx, tx, ready.ImageID, ready.LeaseOwner)
	if err != nil {
		return rollback(tx, err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE card_images
		 SET local_path = $2, content_sha256 = $3, remote_etag = NULLIF($4, ''),
		     remote_last_modified = NULLIF($5, ''), mime_type = $6, width = $7,
		     height = $8, byte_size = $9, status = 'ready', next_attempt_at = NULL,
		     lease_owner = NULL, lease_until = NULL, last_error = NULL, downloaded_at = NOW()
		 WHERE id = $1`,
		ready.ImageID,
		ready.LocalPath,
		ready.ContentSHA256,
		ready.RemoteETag,
		ready.RemoteLastModified,
		ready.MIMEType,
		ready.Width,
		ready.Height,
		ready.ByteSize,
	)
	if err != nil {
		return rollback(tx, fmt.Errorf("catalog: marking image ready: %w", err))
	}

	fingerprints := ready.Fingerprints
	if len(fingerprints) == 0 {
		fingerprints = []ImageFingerprint{{
			Algorithm: FingerprintAlgorithm, AlgorithmVersion: FingerprintAlgorithmVersion,
			Transform: FingerprintTransform, Hash: ready.PHash,
		}}
	}
	for _, fingerprint := range fingerprints {
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO card_fingerprints (image_id, algorithm, algorithm_version, transform, hash)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (image_id, algorithm, algorithm_version, transform)
			 DO UPDATE SET hash = EXCLUDED.hash, created_at = NOW()`,
			ready.ImageID,
			fingerprint.Algorithm,
			fingerprint.AlgorithmVersion,
			fingerprint.Transform,
			fingerprint.Hash,
		)
		if err != nil {
			return rollback(tx, fmt.Errorf("catalog: storing image fingerprint %q: %w", fingerprint.Transform, err))
		}
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE cards SET phash = $2 WHERE id = $1 AND phash IS NULL`,
		cardID,
		ready.PHash,
	)
	if err != nil {
		return rollback(tx, fmt.Errorf("catalog: storing legacy card fingerprint: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: committing ready image: %w", err)
	}
	return nil
}

func (r *PostgresRepository) MarkImageFailed(ctx context.Context, failure ImageFailure) error {
	if err := validateImageFailure(failure); err != nil {
		return err
	}
	status := "failed"
	if failure.Kind == ImageFailureUnavailable {
		status = "unavailable"
	}
	message := failure.Cause.Error()
	const maxErrorLength = 4000
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE card_images
		 SET status = $3, next_attempt_at = $4, lease_owner = NULL,
		     lease_until = NULL, last_error = $5
		 WHERE id = $1 AND status = 'processing' AND lease_owner = $2
		   AND lease_until > NOW()`,
		failure.ImageID,
		failure.LeaseOwner,
		status,
		failure.RetryAt,
		message,
	)
	if err != nil {
		return fmt.Errorf("catalog: marking image failed: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("catalog: reading failed image update result: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("%w: image %d", ErrImageLeaseLost, failure.ImageID)
	}
	return nil
}

func lockImageLease(ctx context.Context, tx *sql.Tx, imageID int64, owner string) (string, error) {
	var cardID string
	var leaseOwner sql.NullString
	err := tx.QueryRowContext(
		ctx,
		`SELECT card_id, lease_owner
		 FROM card_images
		 WHERE id = $1 AND status = 'processing' AND lease_until > NOW()
		 FOR UPDATE`,
		imageID,
	).Scan(&cardID, &leaseOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: image %d", ErrImageLeaseLost, imageID)
	}
	if err != nil {
		return "", fmt.Errorf("catalog: locking image lease: %w", err)
	}
	if !leaseOwner.Valid || leaseOwner.String != owner {
		return "", fmt.Errorf("%w: image %d", ErrImageLeaseLost, imageID)
	}
	return cardID, nil
}

func validateReadyImage(ready ReadyImage) error {
	decodedHash, err := hex.DecodeString(ready.ContentSHA256)
	switch {
	case ready.ImageID <= 0:
		return fmt.Errorf("catalog: image id must be positive")
	case strings.TrimSpace(ready.LeaseOwner) == "":
		return fmt.Errorf("catalog: image lease owner is required")
	case strings.TrimSpace(ready.LocalPath) == "":
		return fmt.Errorf("catalog: local image path is required")
	case err != nil || len(decodedHash) != sha256Size:
		return fmt.Errorf("catalog: image SHA-256 must contain 64 hexadecimal characters")
	case ready.MIMEType != "image/jpeg" && ready.MIMEType != "image/png" && ready.MIMEType != "image/webp":
		return fmt.Errorf("catalog: unsupported ready image MIME type %q", ready.MIMEType)
	case ready.Width <= 0 || ready.Height <= 0:
		return fmt.Errorf("catalog: ready image dimensions must be positive")
	case ready.ByteSize <= 0:
		return fmt.Errorf("catalog: ready image size must be positive")
	}
	seen := make(map[string]struct{}, len(ready.Fingerprints))
	for _, fingerprint := range ready.Fingerprints {
		if fingerprint.Algorithm == "" || fingerprint.AlgorithmVersion <= 0 || fingerprint.Transform == "" {
			return fmt.Errorf("catalog: invalid transformed image fingerprint")
		}
		key := fmt.Sprintf("%s\x00%d\x00%s", fingerprint.Algorithm, fingerprint.AlgorithmVersion, fingerprint.Transform)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("catalog: duplicate transformed image fingerprint %q", fingerprint.Transform)
		}
		seen[key] = struct{}{}
	}
	return nil
}

const sha256Size = 32
