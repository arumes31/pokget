-- Link legacy rows that migration 000025 could not identify because their
-- source IDs differed only by case, their game used a display label, or their
-- language was blank. Keep the update limited to a single unambiguous active
-- canonical printing with the same normalized game and name.
WITH relaxed_candidates AS (
    SELECT legacy.id AS legacy_id, MIN(canonical.id) AS canonical_id
    FROM cards AS legacy
    JOIN cards AS canonical
      ON canonical.source_id IS NOT NULL
     AND canonical.catalog_active = TRUE
     AND canonical.source_card_id IS NOT NULL
     AND LOWER(TRIM(canonical.source_card_id)) = LOWER(TRIM(legacy.id))
     AND canonical.id <> legacy.id
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
    WHERE legacy.source_id IS NULL
      AND legacy.superseded_by_card_id IS NULL
    GROUP BY legacy.id
    HAVING COUNT(*) = 1
)
UPDATE cards AS legacy
SET superseded_by_card_id = candidate.canonical_id
FROM relaxed_candidates AS candidate
WHERE legacy.id = candidate.legacy_id
  AND legacy.superseded_by_card_id IS NULL;
