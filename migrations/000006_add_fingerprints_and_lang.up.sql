-- Add language and visual fingerprint support
ALTER TABLE cards ADD COLUMN IF NOT EXISTS language TEXT DEFAULT 'en';
ALTER TABLE cards ADD COLUMN IF NOT EXISTS phash BIGINT;

-- Index for fast visual lookup
CREATE INDEX IF NOT EXISTS idx_cards_phash ON cards(phash);
