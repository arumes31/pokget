DROP TABLE IF EXISTS card_fingerprints;
DROP TABLE IF EXISTS catalog_printing_images;
DROP TABLE IF EXISTS card_images;
DROP TABLE IF EXISTS catalog_printings;

DROP INDEX IF EXISTS idx_cards_catalog_last_seen_run;
DROP INDEX IF EXISTS idx_cards_catalog_lookup;
DROP INDEX IF EXISTS idx_cards_catalog_source_identity;

ALTER TABLE cards DROP COLUMN IF EXISTS last_seen_run_id;
ALTER TABLE cards DROP COLUMN IF EXISTS last_seen_at;
ALTER TABLE cards DROP COLUMN IF EXISTS first_seen_at;
ALTER TABLE cards DROP COLUMN IF EXISTS catalog_active;
ALTER TABLE cards DROP COLUMN IF EXISTS source_metadata;
ALTER TABLE cards DROP COLUMN IF EXISTS source_updated_at;
ALTER TABLE cards DROP COLUMN IF EXISTS collector_number;
ALTER TABLE cards DROP COLUMN IF EXISTS set_code;
ALTER TABLE cards DROP COLUMN IF EXISTS source_card_id;
ALTER TABLE cards DROP COLUMN IF EXISTS source_id;

DROP TABLE IF EXISTS catalog_sync_runs;
DROP TABLE IF EXISTS catalog_source_state;
DROP TABLE IF EXISTS catalog_sources;
