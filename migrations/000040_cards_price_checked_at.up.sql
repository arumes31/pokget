-- cards.last_updated means "the price changed at": the bulk source only writes a
-- row when the quote differs, and the tracked sync writes every cycle. Users need a
-- different answer - "when was this price last verified?" - otherwise a card whose
-- price has not moved for a month looks stale although it is checked every day.
ALTER TABLE cards ADD COLUMN IF NOT EXISTS price_checked_at TIMESTAMPTZ;

COMMENT ON COLUMN cards.price_checked_at IS
    'last price lookup that returned an answer (a quote or an explicit none); a provider error does not stamp it';

-- Backfill from the known state: every priced card was verified when it was last
-- written, and every row the worker has seen since then carries last_updated.
UPDATE cards SET price_checked_at = last_updated
WHERE price_checked_at IS NULL AND last_updated IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cards_price_checked_at
    ON cards (price_checked_at) WHERE superseded_by_card_id IS NULL;

-- The dashboard records one valuation snapshot per user and hour. The insert checks
-- for a recent row first, which two parallel requests can both pass; the unique
-- index makes the surplus insert a no-op instead of a duplicate data point.
CREATE UNIQUE INDEX IF NOT EXISTS idx_portfolio_history_user_hour
    ON portfolio_history (user_id, (date_trunc('hour', recorded_at)));
