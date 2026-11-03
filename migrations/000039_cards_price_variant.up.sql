-- Scryfall publishes a plain, a foil and an etched price per printing, and some
-- printings only exist as foil. The bulk source falls back to those values so the
-- card is no longer unpriced, but the stored USD value then belongs to another
-- printing. This column records which one: '' (unknown/plain), 'normal', 'foil' or
-- 'etched', so the UI can label the price and no collector reads a foil quote as
-- the value of the normal card.
ALTER TABLE cards ADD COLUMN IF NOT EXISTS price_variant TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN cards.price_variant IS
    'printing the price_usd quote belongs to: empty/normal/foil/etched';
