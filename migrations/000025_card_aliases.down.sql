ALTER TABLE cards DROP CONSTRAINT IF EXISTS cards_superseded_by_card_id_fkey;
DROP INDEX IF EXISTS idx_cards_superseded_by;
ALTER TABLE cards DROP COLUMN IF EXISTS superseded_by_card_id;
