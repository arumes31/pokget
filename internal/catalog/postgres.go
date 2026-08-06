package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	var lastSuccessAt, lastFullSyncAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(cursor, ''), COALESCE(etag, ''), COALESCE(last_modified, ''),
		       COALESCE(upstream_version, ''), last_success_at, last_full_sync_at,
		       last_record_count, COALESCE(last_error, '')
		FROM catalog_source_state
		WHERE source_id = $1`, sourceID).Scan(
		&state.Cursor,
		&state.ETag,
		&state.LastModified,
		&state.UpstreamVersion,
		&lastSuccessAt,
		&lastFullSyncAt,
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

	result, err := tx.ExecContext(
		ctx,
		`UPDATE catalog_source_state
		 SET last_attempt_at = NOW(), updated_at = NOW()
		 WHERE source_id = $1`,
		sourceID,
	)
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: updating source attempt: %w", err))
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return "", rollback(tx, fmt.Errorf("catalog: reading source update result: %w", err))
	}
	if rows == 0 {
		return "", rollback(tx, fmt.Errorf("%w: %q", ErrSourceNotFound, sourceID))
	}

	var runID string
	err = tx.QueryRowContext(
		ctx,
		`INSERT INTO catalog_sync_runs (source_id, mode, status)
		 VALUES ($1, $2, 'running')
		 RETURNING id::text`,
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

	sourceID, mode, err := lockRun(ctx, tx, runID)
	if err != nil {
		return counts, rollback(tx, err)
	}

	isCompleteFullSync := mode == SyncModeFull && completion.Fetch.CompleteSnapshot
	if isCompleteFullSync && !completion.Fetch.NotModified {
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
		 SET cursor = $2, etag = $3, last_modified = $4, upstream_version = $5,
		     last_success_at = NOW(),
		     last_full_sync_at = CASE
		         WHEN $6 = 'full' AND NOT $7 THEN NOW()
		         ELSE last_full_sync_at
		     END,
		     last_record_count = CASE WHEN $7 THEN last_record_count ELSE $8 END,
		     last_error = NULL, consecutive_failures = 0,
		     lease_owner = NULL, lease_until = NULL, updated_at = NOW()
		 WHERE source_id = $1`,
		sourceID,
		completion.Fetch.Cursor,
		completion.Fetch.ETag,
		completion.Fetch.LastModified,
		completion.Fetch.UpstreamVersion,
		mode,
		completion.Fetch.NotModified,
		completion.Fetch.Count,
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
	sourceID, _, err := lockRun(ctx, tx, runID)
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
		     lease_owner = NULL, lease_until = NULL, updated_at = NOW()
		 WHERE source_id = $1`,
		sourceID,
		cause.Error(),
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
		     rarity = EXCLUDED.rarity,
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
		var inserted bool
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
			 RETURNING id, (xmax = 0)`,
			cardID,
			image.SourceImageID,
			kind,
			image.URL,
		).Scan(&imageID, &inserted)
		if err != nil {
			return nil, counts, fmt.Errorf("catalog: upserting image %q: %w", image.SourceImageID, err)
		}

		imageIDs[image.SourceImageID] = imageID
		_, err = u.tx.ExecContext(
			u.ctx,
			`DELETE FROM card_fingerprints
			 WHERE image_id = $1
			   AND EXISTS (
			       SELECT 1 FROM card_images
			       WHERE id = $1 AND status = 'pending' AND content_sha256 IS NULL
			   )`,
			imageID,
		)
		if err != nil {
			return nil, counts, fmt.Errorf("catalog: clearing stale fingerprints for image %q: %w", image.SourceImageID, err)
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
			     rarity = EXCLUDED.rarity,
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
