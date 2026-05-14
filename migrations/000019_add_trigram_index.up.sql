CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_cards_name_trgm ON cards USING gist (name gist_trgm_ops);
