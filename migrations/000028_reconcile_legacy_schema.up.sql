-- Repair databases whose migration version advanced while historical schema
-- objects were absent. Every operation is idempotent so this is safe for both
-- complete installations and affected long-lived volumes.

ALTER TABLE cards ADD COLUMN IF NOT EXISTS game TEXT DEFAULT 'pokemon';
ALTER TABLE cards ADD COLUMN IF NOT EXISTS rarity TEXT;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS language TEXT DEFAULT 'en';
ALTER TABLE cards ADD COLUMN IF NOT EXISTS phash BIGINT;

UPDATE cards SET game = 'pokemon' WHERE game IS NULL OR BTRIM(game) = '';
UPDATE cards SET language = 'en' WHERE language IS NULL OR BTRIM(language) = '';

CREATE TABLE IF NOT EXISTS price_history (
    id SERIAL PRIMARY KEY,
    card_id TEXT NOT NULL,
    price_usd DECIMAL(12, 2),
    price_eur DECIMAL(12, 2),
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE price_history ADD COLUMN IF NOT EXISTS card_id TEXT;
ALTER TABLE price_history ADD COLUMN IF NOT EXISTS price_usd DECIMAL(12, 2);
ALTER TABLE price_history ADD COLUMN IF NOT EXISTS price_eur DECIMAL(12, 2);
ALTER TABLE price_history ADD COLUMN IF NOT EXISTS recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

CREATE TABLE IF NOT EXISTS price_alerts (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    card_id TEXT NOT NULL,
    target_price DECIMAL(12, 2) NOT NULL,
    currency TEXT DEFAULT 'USD',
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS id SERIAL;
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS user_id TEXT;
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS card_id TEXT;
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS target_price DECIMAL(12, 2);
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS currency TEXT DEFAULT 'USD';
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE;
ALTER TABLE price_alerts ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

UPDATE price_alerts SET currency = 'USD' WHERE currency IS NULL OR BTRIM(currency) = '';
UPDATE price_alerts SET is_active = TRUE WHERE is_active IS NULL;

CREATE INDEX IF NOT EXISTS idx_cards_game ON cards(game);
CREATE INDEX IF NOT EXISTS idx_cards_phash ON cards(phash);
CREATE INDEX IF NOT EXISTS idx_price_history_card_id ON price_history(card_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_user_id ON price_alerts(user_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_card_active ON price_alerts(card_id, is_active);
