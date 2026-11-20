package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) (*PostgresRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("catalog: database is required")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) SourceState(ctx context.Context, sourceID string) (SourceState, error) {
	var state SourceState
	state.SourceID = sourceID
	var lastSuccessAt, lastFullSyncAt, lastFullAttemptAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(cursor, ''), COALESCE(etag, ''), COALESCE(last_modified, ''),
		       COALESCE(upstream_version, ''), last_success_at, last_full_sync_at,
		       (SELECT MAX(started_at) FROM catalog_sync_runs
		        WHERE source_id = $1 AND mode = 'full'),
		       last_record_count, COALESCE(last_error, '')
		FROM catalog_source_state
		WHERE source_id = $1`, sourceID).Scan(
		&state.Cursor,
		&state.ETag,
		&state.LastModified,
		&state.UpstreamVersion,
		&lastSuccessAt,
		&lastFullSyncAt,
		&lastFullAttemptAt,
		&state.LastRecordCount,
		&state.LastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceState{}, fmt.Errorf("%w: %q", ErrSourceNotFound, sourceID)
	}
	if err != nil {
		return SourceState{}, fmt.Errorf("catalog: loading source state: %w", err)
	}
	if lastSuccessAt.Valid {
		state.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastFullSyncAt.Valid {
		state.LastFullSyncAt = &lastFullSyncAt.Time
	}
	if lastFullAttemptAt.Valid {
		state.LastFullAttemptAt = &lastFullAttemptAt.Time
	}
	return state, nil
}

func (r *PostgresRepository) BeginRun(
	ctx context.Context,
	sourceID string,
	mode SyncMode,
) (string, error) {
	if sourceID == "" {
		return "", fmt.Errorf("catalog: source id is required")
	}
	if !mode.Valid() {
		return "", fmt.Errorf("catalog: invalid sync mode %q", mode)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("catalog: beginning sync run transaction: %w", err)
	}

	// The app worker and the catalog CLI may sync the same source; the lease
	// lets only one of them run it. It is short and renewed (RenewLease), so a
	// killed process blocks the source for one lease period at most.
	result, err := tx.ExecContext(
		ctx,
		`UPDATE catalog_source_state
		 SET last_attempt_at = NOW(), updated_at = NOW(),
		     lease_owner = NULL, lease_until = NOW() + $2 * INTERVAL '1 second'
		 WHERE source_id = $1 AND (lease_until IS NULL OR lease_until < NOW())`,
		sourceID,
		leaseSeconds,
	)
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: acquiring source lease: %w", err))
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: reading source update result: %w", err))
	}
	if rows == 0 {
		var exists bool
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM catalog_source_state WHERE source_id = $1)`, sourceID,
		).Scan(&exists)
		switch {
		case err != nil:
			return "", rollback(tx, fmt.Errorf("catalog: checking source state: %w", err))
		case exists:
			return "", rollback(tx, fmt.Errorf("%w: %q", ErrSourceBusy, sourceID))
		default:
			return "", rollback(tx, fmt.Errorf("%w: %q", ErrSourceNotFound, sourceID))
		}
	}

	// The lease was free, so a run still marked running lost its process.
	_, err = tx.ExecContext(
		ctx,
		`UPDATE catalog_sync_runs
		 SET status = 'failed', finished_at = NOW(), error = 'abandoned: the sync process stopped'
		 WHERE source_id = $1 AND status = 'running'`,
		sourceID,
	)
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: closing abandoned sync runs: %w", err))
	}

	var runID string
	err = tx.QueryRowContext(
		ctx,
		`WITH run AS (
		     INSERT INTO catalog_sync_runs (source_id, mode, status)
		     VALUES ($1, $2, 'running')
		     RETURNING id
		 ), lease AS (
		     UPDATE catalog_source_state
		     SET lease_owner = (SELECT id::text FROM run)
		     WHERE source_id = $1
		 )
		 SELECT id::text FROM run`,
		sourceID,
		mode,
	).Scan(&runID)
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: inserting sync run: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("catalog: committing sync run: %w", err)
	}
	return runID, nil
}

// RunLeaseTTL is how long a source lease lasts without renewal. Syncer renews
// it every RunLeaseTTL/5 while a run is active.
const RunLeaseTTL = 10 * time.Minute

var leaseSeconds = int(RunLeaseTTL / time.Second)

// RenewLease extends the source lease held by runID. ErrLeaseLost means another
// process took the source over after the lease expired.
func (r *PostgresRepository) RenewLease(ctx context.Context, runID string) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE catalog_source_state
		 SET lease_until = NOW() + $2 * INTERVAL '1 second'
		 WHERE lease_owner = $1`,
		runID,
		leaseSeconds,
	)
	if err != nil {
		return fmt.Errorf("catalog: renewing source lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("catalog: reading lease renewal result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: run %q", ErrLeaseLost, runID)
	}
	return nil
}

// UpsertBatch writes one batch in a transaction and retries when the transaction
// lost a lock race. The price worker updates the same card rows (bulk prices, EUR
// conversion, per-card backfill), so a deadlock used to abort a whole source sync
// after minutes of work.
func (r *PostgresRepository) UpsertBatch(
	ctx context.Context,
	batch Batch,
) (ChangeCounts, error) {
	var counts ChangeCounts
	if err := validateBatch(batch); err != nil {
		return counts, err
	}
	if len(batch.Records) == 0 {
		return counts, nil
	}

	var lastErr error
	for attempt := 0; attempt <= catalogUpsertAttempts; attempt++ {
		counts, lastErr = r.upsertBatchOnce(ctx, batch)
		if lastErr == nil {
			return counts, nil
		}
		if !retryableCatalogError(lastErr) || attempt == catalogUpsertAttempts {
			// No wait after the final attempt: the database error is the useful one,
			// and a cancelled context must not replace it.
			return counts, lastErr
		}
		slog.Warn("Catalog: retrying batch after a lock conflict",
			"source", batch.SourceID, "game", batch.Game, "attempt", attempt+1, "error", lastErr)
		if !waitForCatalogRetry(ctx, time.Duration(attempt+1)*catalogUpsertBackoff) {
			return ChangeCounts{}, ctx.Err()
		}
	}
	return ChangeCounts{}, lastErr
}

func (r *PostgresRepository) upsertBatchOnce(ctx context.Context, batch Batch) (ChangeCounts, error) {
	var counts ChangeCounts
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return counts, fmt.Errorf("catalog: beginning batch transaction: %w", err)
	}
	if err := verifyRun(ctx, tx, batch); err != nil {
		return counts, rollback(tx, err)
	}

	upsert := upsertContext{
		ctx:      ctx,
		tx:       tx,
		runID:    batch.RunID,
		sourceID: batch.SourceID,
		game:     batch.Game,
	}
	for _, record := range batch.Records {
		cardID, inserted, err := upsert.card(record)
		if err != nil {
			return counts, rollback(tx, err)
		}
		if inserted {
			counts.CardsInserted++
		} else {
			counts.CardsUpdated++
		}

		imageIDs, imageCounts, err := upsert.images(cardID, record.Images)
		if err != nil {
			return counts, rollback(tx, err)
		}
		counts.ImagesInserted += imageCounts.ImagesInserted
		counts.ImagesUpdated += imageCounts.ImagesUpdated

		printingCounts, err := upsert.printings(cardID, record, imageIDs)
		if err != nil {
			return counts, rollback(tx, err)
		}
		counts.PrintingsInserted += printingCounts.PrintingsInserted
		counts.PrintingsUpdated += printingCounts.PrintingsUpdated
	}

	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("catalog: committing batch: %w", err)
	}
	return counts, nil
}

// catalogUpsertAttempts is how often one batch is retried after a lock conflict.
// The price workers write the same rows, so a conflict is expected work, not a
// failure.
const (
	catalogUpsertAttempts = 4
	catalogUpsertBackoff  = 250 * time.Millisecond
)

// retryableCatalogError reports whether the batch lost a lock race and may be
// retried: 40P01 deadlock_detected, 40001 serialization_failure, 55P03
// lock_not_available.
func retryableCatalogError(err error) bool {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40P01" || pgErr.Code == "40001" || pgErr.Code == "55P03"
}

func waitForCatalogRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *PostgresRepository) CompleteRun(
	ctx context.Context,
	runID string,
	completion Completion,
) (ChangeCounts, error) {
	counts := completion.Changes
	if runID == "" {
		return counts, fmt.Errorf("catalog: run id is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return counts, fmt.Errorf("catalog: beginning completion transaction: %w", err)
	}

	sourceID, mode, previous, err := lockSourceAndRun(ctx, tx, runID)
	if err != nil {
		return counts, rollback(tx, err)
	}

	complete := completion.Fetch.CompleteSnapshot
	// Checked for incremental runs too: they set the baseline the next full run is
	// compared with.
	if complete && !completion.Fetch.NotModified {
		complete = !shrunkSnapshot(completion.Fetch.Count, previous)
		if !complete {
			// A source that claims completeness after losing half its catalog
			// more likely failed silently; a genuine shrink needs an operator.
			slog.Warn("Catalog: snapshot shrank by more than half, treating it as partial (no deactivation)",
				"source", sourceID, "mode", mode, "records", completion.Fetch.Count, "previous_records", previous)
		}
	}
	if mode == SyncModeFull && complete && !completion.Fetch.NotModified {
		cards, err := deactivateUnseenCards(ctx, tx, sourceID, runID)
		if err != nil {
			return counts, rollback(tx, err)
		}
		printings, err := deactivateUnseenPrintings(ctx, tx, sourceID, runID)
		if err != nil {
			return counts, rollback(tx, err)
		}
		counts.CardsDeactivated += cards
		counts.PrintingsDeactivated += printings
	}

	status := "succeeded"
	if completion.Fetch.NotModified {
		status = "not_modified"
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE catalog_sync_runs
		 SET status = $2, finished_at = NOW(), upstream_version = $3,
		     fetched_count = $4, inserted_count = $5, updated_count = $6,
		     deactivated_count = $7, image_count = $8, error = NULL
		 WHERE id = $1`,
		runID,
		status,
		completion.Fetch.UpstreamVersion,
		completion.Fetch.Count,
		counts.CardsInserted+counts.PrintingsInserted,
		counts.CardsUpdated+counts.PrintingsUpdated,
		counts.CardsDeactivated+counts.PrintingsDeactivated,
		counts.ImagesInserted+counts.ImagesUpdated,
	)
	if err != nil {
		return counts, rollback(tx, fmt.Errorf("catalog: completing sync run: %w", err))
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE catalog_source_state
		 SET cursor = $2,
		     -- A partial snapshot skipped data: keeping its validators would
		     -- answer the next run with "not modified" and never refetch it.
		     etag = CASE WHEN $9 OR $7 THEN $3 ELSE NULL END,
		     last_modified = CASE WHEN $9 OR $7 THEN $4 ELSE NULL END,
		     upstream_version = $5,
		     last_success_at = NOW(),
		     last_full_sync_at = CASE
		         WHEN $6 = 'full' AND NOT $7 AND $9 THEN NOW()
		         ELSE last_full_sync_at
		     END,
		     -- Only a complete snapshot is a baseline for the shrink guard.
		     last_record_count = CASE WHEN NOT $7 AND $9 THEN $8 ELSE last_record_count END,
		     last_error = NULL, consecutive_failures = 0,
		     lease_owner = CASE WHEN lease_owner = $10 THEN NULL ELSE lease_owner END,
		     lease_until = CASE WHEN lease_owner = $10 THEN NULL ELSE lease_until END,
		     updated_at = NOW()
		 WHERE source_id = $1`,
		sourceID,
		completion.Fetch.Cursor,
		completion.Fetch.ETag,
		completion.Fetch.LastModified,
		completion.Fetch.UpstreamVersion,
		mode,
		completion.Fetch.NotModified,
		completion.Fetch.Count,
		complete,
		runID,
	)
	if err != nil {
		return counts, rollback(tx, fmt.Errorf("catalog: updating successful source state: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("catalog: committing sync completion: %w", err)
	}
	return counts, nil
}

func (r *PostgresRepository) FailRun(ctx context.Context, runID string, cause error) error {
	if runID == "" {
		return fmt.Errorf("catalog: run id is required")
	}
	if cause == nil {
		return fmt.Errorf("catalog: failure cause is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: beginning failure transaction: %w", err)
	}
	sourceID, _, _, err := lockSourceAndRun(ctx, tx, runID)
	if err != nil {
		return rollback(tx, err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE catalog_sync_runs
		 SET status = 'failed', finished_at = NOW(), error = $2
		 WHERE id = $1`,
		runID,
		cause.Error(),
	)
	if err != nil {
		return rollback(tx, fmt.Errorf("catalog: failing sync run: %w", err))
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE catalog_source_state
		 SET last_error = $2, consecutive_failures = consecutive_failures + 1,
		     lease_owner = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_owner END,
		     lease_until = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_until END,
		     updated_at = NOW()
		 WHERE source_id = $1`,
		sourceID,
		cause.Error(),
		runID,
	)
	if err != nil {
		return rollback(tx, fmt.Errorf("catalog: updating failed source state: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: committing sync failure: %w", err)
	}
	return nil
}

type upsertContext struct {
	ctx      context.Context
	tx       *sql.Tx
	runID    string
	sourceID string
	game     Game
}

func (u upsertContext) card(record CardRecord) (string, bool, error) {
	cardID, err := CardID(u.sourceID, record.SourceCardID, record.Language)
	if err != nil {
		return "", false, fmt.Errorf("catalog: creating card id: %w", err)
	}

	imageURL := ""
	if len(record.Images) > 0 {
		imageURL = record.Images[0].URL
	}

	var inserted bool
	err = u.tx.QueryRowContext(
		u.ctx,
		`INSERT INTO cards (
		     id, name, set_name, image_url, price_usd, price_eur, variant,
		     language, game, rarity, source_id, source_card_id, set_code,
		     collector_number, source_updated_at, source_metadata,
		     catalog_active, first_seen_at, last_seen_at, last_seen_run_id
		 ) VALUES (
		     $1, $2, $3, NULLIF($4, ''), 0, 0, $5,
		     $6, $7, NULLIF($8, ''), $9, $10, NULLIF($11, ''),
		     NULLIF($12, ''), $13, $14::jsonb,
		     TRUE, NOW(), NOW(), $15::uuid
		 )
		 ON CONFLICT (id) DO UPDATE SET
		     name = EXCLUDED.name,
		     set_name = EXCLUDED.set_name,
		     image_url = COALESCE(EXCLUDED.image_url, cards.image_url),
		     variant = EXCLUDED.variant,
		     language = EXCLUDED.language,
		     game = EXCLUDED.game,
		     -- Sources that publish no rarity (TCGdex lists sets without one) must not
		     -- erase a rarity the price sync filled in from the marketplace payload.
		     rarity = COALESCE(EXCLUDED.rarity, cards.rarity),
		     source_id = EXCLUDED.source_id,
		     source_card_id = EXCLUDED.source_card_id,
		     set_code = EXCLUDED.set_code,
		     collector_number = EXCLUDED.collector_number,
		     source_updated_at = EXCLUDED.source_updated_at,
		     source_metadata = EXCLUDED.source_metadata,
		     catalog_active = TRUE,
		     last_seen_at = NOW(),
		     last_seen_run_id = EXCLUDED.last_seen_run_id
		 RETURNING (xmax = 0)`,
		cardID,
		record.Name,
		record.SetName,
		imageURL,
		defaultVariant(record.Variant),
		record.Language,
		u.game,
		record.Rarity,
		u.sourceID,
		record.SourceCardID,
		record.SetCode,
		record.CollectorNumber,
		record.SourceUpdatedAt,
		metadataString(record.Metadata),
		u.runID,
	).Scan(&inserted)
	if err != nil {
		return "", false, fmt.Errorf("catalog: upserting card %q: %w", record.SourceCardID, err)
	}

	return cardID, inserted, nil
}

func (u upsertContext) images(
	cardID string,
	images []ImageRecord,
) (map[string]int64, ChangeCounts, error) {
	imageIDs := make(map[string]int64, len(images))
	var counts ChangeCounts
	for _, image := range images {
		kind := defaultString(image.Kind, "front")
		var imageID int64
		var inserted, reset bool
		err := u.tx.QueryRowContext(
			u.ctx,
			`INSERT INTO card_images (
			     card_id, source_image_id, kind, remote_url, status,
			     first_seen_at, last_seen_at
			 ) VALUES ($1, $2, $3, $4, 'pending', NOW(), NOW())
			 ON CONFLICT (card_id, source_image_id) DO UPDATE SET
			     kind = EXCLUDED.kind,
			     remote_url = EXCLUDED.remote_url,
			     local_path = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.local_path
			     END,
			     content_sha256 = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.content_sha256
			     END,
			     remote_etag = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.remote_etag
			     END,
			     remote_last_modified = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.remote_last_modified
			     END,
			     mime_type = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.mime_type
			     END,
			     width = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.width
			     END,
			     height = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.height
			     END,
			     byte_size = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.byte_size
			     END,
			     status = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN 'pending'
			         ELSE card_images.status
			     END,
			     attempts = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN 0
			         ELSE card_images.attempts
			     END,
			     next_attempt_at = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.next_attempt_at
			     END,
			     last_error = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.last_error
			     END,
			     downloaded_at = CASE
			         WHEN card_images.remote_url IS DISTINCT FROM EXCLUDED.remote_url THEN NULL
			         ELSE card_images.downloaded_at
			     END,
			     last_seen_at = NOW()
			 RETURNING id, (xmax = 0), (status = 'pending' AND content_sha256 IS NULL)`,
			cardID,
			image.SourceImageID,
			kind,
			image.URL,
		).Scan(&imageID, &inserted, &reset)
		if err != nil {
			return nil, counts, fmt.Errorf("catalog: upserting image %q: %w", image.SourceImageID, err)
		}

		imageIDs[image.SourceImageID] = imageID
		// A new image has no fingerprints, and a ready one keeps them; only an
		// existing image without content (URL changed) can hold stale ones.
		if !inserted && reset {
			_, err = u.tx.ExecContext(u.ctx, `DELETE FROM card_fingerprints WHERE image_id = $1`, imageID)
			if err != nil {
				return nil, counts, fmt.Errorf("catalog: clearing stale fingerprints for image %q: %w", image.SourceImageID, err)
			}
		}

		if inserted {
			counts.ImagesInserted++
		} else {
			counts.ImagesUpdated++
		}
	}
	return imageIDs, counts, nil
}

func (u upsertContext) printings(
	cardID string,
	card CardRecord,
	imageIDs map[string]int64,
) (ChangeCounts, error) {
	var counts ChangeCounts
	for _, printing := range card.Printings {
		language := defaultString(printing.Language, card.Language)
		variant := defaultVariant(printing.Variant)
		printingID, err := PrintingID(
			u.sourceID,
			printing.SourcePrintingID,
			language,
			variant,
		)
		if err != nil {
			return counts, fmt.Errorf("catalog: creating printing id: %w", err)
		}

		var inserted bool
		err = u.tx.QueryRowContext(
			u.ctx,
			`INSERT INTO catalog_printings (
			     id, card_id, source_id, source_printing_id, set_code, set_name,
			     collector_number, rarity, language, variant, released_at,
			     source_metadata, catalog_active, first_seen_at, last_seen_at,
			     last_seen_run_id
			 ) VALUES (
			     $1, $2, $3, $4, NULLIF($5, ''), $6,
			     NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11,
			     $12::jsonb, TRUE, NOW(), NOW(), $13::uuid
			 )
			 ON CONFLICT (id) DO UPDATE SET
			     card_id = EXCLUDED.card_id,
			     set_code = EXCLUDED.set_code,
			     set_name = EXCLUDED.set_name,
			     collector_number = EXCLUDED.collector_number,
			     rarity = COALESCE(EXCLUDED.rarity, catalog_printings.rarity),
			     language = EXCLUDED.language,
			     variant = EXCLUDED.variant,
			     released_at = EXCLUDED.released_at,
			     source_metadata = EXCLUDED.source_metadata,
			     catalog_active = TRUE,
			     last_seen_at = NOW(),
			     last_seen_run_id = EXCLUDED.last_seen_run_id
			 RETURNING (xmax = 0)`,
			printingID,
			cardID,
			u.sourceID,
			printing.SourcePrintingID,
			printing.SetCode,
			printing.SetName,
			printing.CollectorNumber,
			printing.Rarity,
			language,
			variant,
			printing.ReleasedAt,
			metadataString(printing.Metadata),
			u.runID,
		).Scan(&inserted)
		if err != nil {
			return counts, fmt.Errorf("catalog: upserting printing %q: %w", printing.SourcePrintingID, err)
		}

		if inserted {
			counts.PrintingsInserted++
		} else {
			counts.PrintingsUpdated++
		}

		_, err = u.tx.ExecContext(
			u.ctx,
			`DELETE FROM catalog_printing_images WHERE printing_id = $1`,
			printingID,
		)
		if err != nil {
			return counts, fmt.Errorf("catalog: clearing image links for printing %q: %w", printing.SourcePrintingID, err)
		}

		for _, sourceImageID := range printing.SourceImageIDs {
			imageID := imageIDs[sourceImageID]
			_, err := u.tx.ExecContext(
				u.ctx,
				`INSERT INTO catalog_printing_images (printing_id, image_id)
				 VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`,
				printingID,
				imageID,
			)
			if err != nil {
				return counts, fmt.Errorf("catalog: linking printing %q to image %q: %w", printing.SourcePrintingID, sourceImageID, err)
			}
		}
	}
	return counts, nil
}

func validateBatch(batch Batch) error {
	switch {
	case batch.RunID == "":
		return fmt.Errorf("catalog: run id is required")
	case batch.SourceID == "":
		return fmt.Errorf("catalog: source id is required")
	case !batch.Game.Valid():
		return fmt.Errorf("catalog: invalid game %q", batch.Game)
	}
	for _, record := range batch.Records {
		if err := record.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func verifyRun(ctx context.Context, tx *sql.Tx, batch Batch) error {
	sourceID, game, err := runSource(ctx, tx, batch.RunID)
	if err != nil {
		return err
	}
	if sourceID != batch.SourceID {
		return fmt.Errorf("catalog: run source %q does not match batch source %q", sourceID, batch.SourceID)
	}
	if Game(game) != batch.Game {
		return fmt.Errorf("catalog: source game %q does not match batch game %q", game, batch.Game)
	}
	return nil
}

// lockSourceAndRun locks the source state row before the run row, the same
// order BeginRun uses (state lease, then abandoned runs); the opposite order
// let a takeover and a completion deadlock. It also returns last_record_count.
func lockSourceAndRun(ctx context.Context, tx *sql.Tx, runID string) (string, SyncMode, int64, error) {
	var stateSourceID string
	var previous int64
	err := tx.QueryRowContext(
		ctx,
		`SELECT source_id, last_record_count
		 FROM catalog_source_state
		 WHERE source_id = (SELECT source_id FROM catalog_sync_runs WHERE id = $1)
		 FOR UPDATE`,
		runID,
	).Scan(&stateSourceID, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, fmt.Errorf("%w: %q", ErrRunNotRunning, runID)
	}
	if err != nil {
		return "", "", 0, fmt.Errorf("catalog: locking source state: %w", err)
	}
	// lockRun still requires status 'running', so a displaced run cannot write.
	sourceID, mode, err := lockRun(ctx, tx, runID)
	if err != nil {
		return "", "", 0, err
	}
	if sourceID != stateSourceID {
		return "", "", 0, fmt.Errorf("catalog: run %q changed source", runID)
	}
	return sourceID, mode, previous, nil
}

func lockRun(ctx context.Context, tx *sql.Tx, runID string) (string, SyncMode, error) {
	var sourceID string
	var mode SyncMode
	err := tx.QueryRowContext(
		ctx,
		`SELECT source_id, mode
		 FROM catalog_sync_runs
		 WHERE id = $1 AND status = 'running'
		 FOR UPDATE`,
		runID,
	).Scan(&sourceID, &mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: %q", ErrRunNotRunning, runID)
	}
	if err != nil {
		return "", "", fmt.Errorf("catalog: locking sync run: %w", err)
	}
	return sourceID, mode, nil
}

func runSource(ctx context.Context, tx *sql.Tx, runID string) (string, string, error) {
	var sourceID string
	var game string
	err := tx.QueryRowContext(
		ctx,
		`SELECT run.source_id, source.game
		 FROM catalog_sync_runs AS run
		 JOIN catalog_sources AS source ON source.id = run.source_id
		 WHERE run.id = $1 AND run.status = 'running'
		 FOR UPDATE OF run`,
		runID,
	).Scan(&sourceID, &game)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: %q", ErrRunNotRunning, runID)
	}
	if err != nil {
		return "", "", fmt.Errorf("catalog: loading sync run source: %w", err)
	}
	return sourceID, game, nil
}

func deactivateUnseenCards(
	ctx context.Context,
	tx *sql.Tx,
	sourceID string,
	runID string,
) (int64, error) {
	result, err := tx.ExecContext(
		ctx,
		`UPDATE cards
		 SET catalog_active = FALSE
		 WHERE source_id = $1 AND catalog_active = TRUE
		   AND last_seen_run_id IS DISTINCT FROM $2::uuid`,
		sourceID,
		runID,
	)
	if err != nil {
		return 0, fmt.Errorf("catalog: deactivating unseen cards: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("catalog: reading deactivated card count: %w", err)
	}
	return count, nil
}

func deactivateUnseenPrintings(
	ctx context.Context,
	tx *sql.Tx,
	sourceID string,
	runID string,
) (int64, error) {
	result, err := tx.ExecContext(
		ctx,
		`UPDATE catalog_printings
		 SET catalog_active = FALSE
		 WHERE source_id = $1 AND catalog_active = TRUE
		   AND last_seen_run_id IS DISTINCT FROM $2::uuid`,
		sourceID,
		runID,
	)
	if err != nil {
		return 0, fmt.Errorf("catalog: deactivating unseen printings: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("catalog: reading deactivated printing count: %w", err)
	}
	return count, nil
}

// shrunkSnapshot reports whether a complete snapshot holds less than half of the
// previous complete snapshot. A first sync (previous 0) is never shrunk.
func shrunkSnapshot(count, previous int64) bool {
	return previous > 0 && count*2 < previous
}

func metadataString(metadata json.RawMessage) string {
	if len(metadata) == 0 {
		return "{}"
	}
	return string(metadata)
}

func rollback(tx *sql.Tx, cause error) error {
	err := tx.Rollback()
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("catalog: rolling back transaction: %w", err))
}
