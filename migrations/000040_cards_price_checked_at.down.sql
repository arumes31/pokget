DROP INDEX IF EXISTS idx_cards_price_checked_at;
ALTER TABLE cards DROP COLUMN IF EXISTS price_checked_at;
