-- Undo only aliases that require the relaxed comparison introduced by 000027;
-- aliases created by 000025 satisfy all three strict comparisons and remain.
WITH relaxed_aliases AS (
    SELECT legacy.id AS legacy_id
    FROM cards AS legacy
    JOIN cards AS canonical ON canonical.id = legacy.superseded_by_card_id
    WHERE legacy.source_id IS NULL
      AND canonical.source_id IS NOT NULL
      AND canonical.source_card_id IS NOT NULL
      AND LOWER(TRIM(canonical.source_card_id)) = LOWER(TRIM(legacy.id))
      AND REGEXP_REPLACE(LOWER(TRIM(COALESCE(canonical.game, ''))), '[^a-z0-9]+', '', 'g') =
          REGEXP_REPLACE(LOWER(TRIM(COALESCE(legacy.game, ''))), '[^a-z0-9]+', '', 'g')
      AND LOWER(TRIM(canonical.name)) = LOWER(TRIM(legacy.name))
      AND (
          NULLIF(TRIM(COALESCE(legacy.language, '')), '') IS NULL
          OR LOWER(TRIM(canonical.language)) = LOWER(TRIM(legacy.language))
      )
      AND (
          canonical.source_card_id <> legacy.id
          OR LOWER(TRIM(COALESCE(canonical.game, ''))) <> LOWER(TRIM(COALESCE(legacy.game, '')))
          OR LOWER(TRIM(COALESCE(canonical.language, ''))) <> LOWER(TRIM(COALESCE(legacy.language, '')))
      )
)
UPDATE cards AS legacy
SET superseded_by_card_id = NULL
FROM relaxed_aliases AS alias
WHERE legacy.id = alias.legacy_id;
