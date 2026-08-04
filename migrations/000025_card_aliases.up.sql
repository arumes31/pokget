ALTER TABLE cards ADD COLUMN IF NOT EXISTS superseded_by_card_id TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'cards_superseded_by_card_id_fkey'
    ) THEN
        ALTER TABLE cards
            ADD CONSTRAINT cards_superseded_by_card_id_fkey
            FOREIGN KEY (superseded_by_card_id) REFERENCES cards(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_cards_superseded_by
    ON cards(superseded_by_card_id)
    WHERE superseded_by_card_id IS NOT NULL;

WITH unambiguous_aliases AS (
    SELECT legacy.id AS legacy_id, MIN(canonical.id) AS canonical_id
    FROM cards AS legacy
    JOIN cards AS canonical
      ON canonical.source_card_id = legacy.id
     AND canonical.source_id IS NOT NULL
     AND canonical.catalog_active = TRUE
     AND canonical.id <> legacy.id
     AND LOWER(TRIM(COALESCE(canonical.game, ''))) = LOWER(TRIM(COALESCE(legacy.game, '')))
     AND LOWER(TRIM(canonical.name)) = LOWER(TRIM(legacy.name))
     AND LOWER(TRIM(COALESCE(canonical.language, ''))) = LOWER(TRIM(COALESCE(legacy.language, '')))
    WHERE legacy.source_id IS NULL
    GROUP BY legacy.id
    HAVING COUNT(*) = 1
)
UPDATE cards AS legacy
SET superseded_by_card_id = aliases.canonical_id
FROM unambiguous_aliases AS aliases
WHERE legacy.id = aliases.legacy_id
  AND legacy.superseded_by_card_id IS NULL;
