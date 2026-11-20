package worker

import (
	"context"
	"errors"
	"log/slog"

	"pokget/internal/models"
	"pokget/internal/service"
)

// errNoBackfillPrice reports that no configured provider knew the card.
var errNoBackfillPrice = errors.New("price backfill: no provider returned a price")

// defaultBackfillRate is the request rate used when none is configured. Without it
// a zero rate would mean "unlimited" and hammer the provider.
const defaultBackfillRate = 2.0

// backfillProgressEvery is how many cards are walked between two progress lines.
const backfillProgressEvery = 500

func backfillRate(configured float64) float64 {
	if configured <= 0 {
		return defaultBackfillRate
	}
	return configured
}

// backfillPrices fills prices for Pokémon catalog cards without a price. The bulk
// sources cover most games, but the Pokémon catalog (TCGdex) only knows prices per
// card, so the long tail of a language the marketplace does not list (German,
// French, Spanish, Chinese, Korean printings) stays unpriced without this pass.
// Owned cards are included: the tracked-card sync only prices what it reaches
// within its budget, and the backfill is the second chance.
//
// The pass is deliberately bounded: it walks `PRICE_BACKFILL_BATCH` cards per cycle,
// and a card the source has no price for moves to the back of the queue instead of
// blocking the rest.
func (w *DataSyncWorker) backfillPrices(ctx context.Context) {
	if w.backfillBatch <= 0 {
		return
	}
	// A pass can outlast the price ticker; a second concurrent walk would only
	// duplicate requests.
	if !w.backfillRunning.CompareAndSwap(false, true) {
		return
	}
	defer w.backfillRunning.Store(false)
	clients := w.priceClients[models.NormalizeGame("pokemon")]
	if len(clients) == 0 {
		return
	}
	if ctx.Err() != nil {
		return
	}

	// Two queues share the batch turn by turn. Cards without USD go least recently
	// verified first: a miss stamps them and moves them to the back. Cards that only
	// lack a Cardmarket EUR (none, or one converted from USD) already show a USD
	// price, and stamping a miss on them would claim that USD was verified, so they
	// rotate by id after a cursor instead.
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, name, set_name, game, language, source_id, source_card_id, set_code,
		       collector_number, has_usd
		FROM (
			SELECT id, name, set_name, COALESCE(game, '') AS game, COALESCE(language, '') AS language,
			       COALESCE(source_id, '') AS source_id, COALESCE(source_card_id, '') AS source_card_id,
			       COALESCE(set_code, '') AS set_code, COALESCE(collector_number, '') AS collector_number,
			       COALESCE(price_usd, 0) > 0 AS has_usd,
			       ROW_NUMBER() OVER (
			           PARTITION BY COALESCE(price_usd, 0) > 0
			           ORDER BY CASE WHEN COALESCE(price_usd, 0) > 0 THEN id END,
			                    price_checked_at ASC NULLS FIRST, id) AS turn
			FROM cards
			WHERE superseded_by_card_id IS NULL
			  AND game = 'pokemon'
			  AND (COALESCE(price_usd, 0) <= 0
			       OR ((COALESCE(price_eur, 0) <= 0 OR price_eur_estimated) AND id > $2))
			  AND COALESCE(source_card_id, '') <> ''
		) AS queue
		ORDER BY turn, has_usd
		LIMIT $1`, w.backfillBatch, w.backfillCursor)
	if err != nil {
		slog.Warn("Price backfill: query failed", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	cards := make([]models.Card, 0, w.backfillBatch)
	cursor, withUSD := w.backfillCursor, 0
	for rows.Next() {
		var card models.Card
		var hasUSD bool
		if err := rows.Scan(&card.ID, &card.Name, &card.Set, &card.Game, &card.Language,
			&card.SourceID, &card.SourceCardID, &card.SetCode, &card.CollectorNumber, &hasUSD); err != nil {
			slog.Warn("Price backfill: scan failed", "error", err)
			continue
		}
		if hasUSD {
			cursor = card.ID
			withUSD++
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("Price backfill: read failed", "error", err)
	}
	_ = rows.Close()
	// The USD queue ran short of its turns: it reached the end of the id range and
	// the next pass starts over.
	if len(cards) < w.backfillBatch || withUSD < len(cards)/2 {
		cursor = ""
	}
	w.backfillCursor = cursor
	if len(cards) == 0 {
		return
	}

	var priced, missing int64
	for index, card := range cards {
		if ctx.Err() != nil {
			break
		}
		// A pass takes about an hour, so log progress instead of staying silent
		// until the summary line.
		if index > 0 && index%backfillProgressEvery == 0 {
			slog.Info("Price backfill progress", "done", index, "candidates", len(cards),
				"priced", priced, "without_price", missing)
		}
		if w.backfillLimiter != nil {
			if err := w.backfillLimiter.Wait(ctx); err != nil {
				break
			}
		}
		usd, eur, err := fetchCardPriceFrom(ctx, clients, card)
		if err != nil {
			// A provider that answered "no price" is a verification and moves the card
			// to the back; a transport or provider error is not and must be retried on
			// the next cycle with its stamp untouched.
			if errors.Is(err, errNoBackfillPrice) {
				w.markBackfillMiss(ctx, card.ID)
			}
			missing++
			continue
		}
		applied, err := w.storeBackfillPrice(ctx, card.ID, usd, eur)
		if err != nil {
			slog.Warn("Price backfill: store failed", "card_id", card.ID, "error", err)
			continue
		}
		if !applied {
			// The provider answered only in a currency the card already has; nothing
			// changed, so rotate the card to the back instead of retrying it forever.
			w.markBackfillMiss(ctx, card.ID)
			missing++
			continue
		}
		priced++
	}
	slog.Info("Price backfill finished", "candidates", len(cards), "priced", priced, "without_price", missing)
	if priced > 0 && w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

// markBackfillMiss moves a card to the back of the queue after a successful lookup
// that found no price, without touching its (empty) prices. A card with a USD price
// is skipped: the stamp would claim that USD was verified, and that queue rotates
// through backfillCursor instead.
func (w *DataSyncWorker) markBackfillMiss(ctx context.Context, cardID string) {
	if _, err := w.db.ExecContext(ctx,
		`UPDATE cards SET price_checked_at = NOW() WHERE id = $1 AND COALESCE(price_usd, 0) <= 0`, cardID); err != nil {
		slog.Debug("Price backfill: could not record the miss", "card_id", cardID, "error", err)
	}
}

// storeBackfillPrice writes a market price the same way the per-card sync does.
//
// The walk is slow, so the bulk or tracked-card sync may have priced the card in
// the meantime: the statement rechecks that a currency is still missing, keeps a
// currency the provider did not return and only records history for the row it
// actually changed. Card and history are written in one transaction.
//
// It reports whether a price was actually filled: a card whose only provider
// answer repeats a currency it already has made no progress and has to move to the
// back of the queue instead of being retried forever.
func (w *DataSyncWorker) storeBackfillPrice(ctx context.Context, cardID string, usd, eur float64) (bool, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	// The parameters are cast explicitly: an untyped parameter that first appears
	// in `$1 > 0` is inferred as integer and rejects "2.15".
	result, err := tx.ExecContext(ctx, `
		UPDATE cards
		SET price_usd = CASE WHEN $1::numeric > 0 THEN $1::numeric ELSE price_usd END,
		    price_eur = CASE WHEN $2::numeric > 0 THEN $2::numeric ELSE price_eur END,
		    price_eur_estimated = CASE WHEN $2::numeric > 0 THEN FALSE ELSE price_eur_estimated END,
		    last_updated = NOW(), price_checked_at = NOW()
		WHERE id = $3
		  AND ((COALESCE(price_usd, 0) <= 0 AND $1::numeric > 0)
		    OR ((COALESCE(price_eur, 0) <= 0 OR price_eur_estimated) AND $2::numeric > 0))`, usd, eur, cardID)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if changed == 0 {
		_ = tx.Rollback()
		return false, nil
	}
	// Deliberately no price_history row: the backfill only fills a currency that was
	// missing, so a history entry would record the same moment twice and add a
	// hundred thousand meaningless points to the chart. The first real change (sync
	// or a later backfill of a new printing) starts the series.
	return true, tx.Commit()
}

// fetchCardPriceFrom walks a provider chain until one of them knows the card. It
// reports errNoBackfillPrice only when every provider answered "no price"; any
// transport or provider error wins so the card is retried.
func fetchCardPriceFrom(ctx context.Context, clients []service.PriceClient, card models.Card) (float64, float64, error) {
	var lastErr error
	for _, client := range clients {
		if client == nil {
			continue
		}
		var (
			usd, eur float64
			err      error
		)
		if contextClient, ok := client.(service.ContextPriceClient); ok {
			usd, eur, err = contextClient.FetchPriceContext(ctx, card)
		} else {
			usd, eur, err = client.FetchPrice(card)
		}
		if err != nil {
			if !errors.Is(err, service.ErrNoPrice) {
				lastErr = err
			}
			continue
		}
		if usd > 0 || eur > 0 {
			return usd, eur, nil
		}
	}
	if lastErr == nil {
		lastErr = errNoBackfillPrice
	}
	return 0, 0, lastErr
}
