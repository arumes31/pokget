CREATE TABLE IF NOT EXISTS catalog_sources (
    id TEXT PRIMARY KEY,
    game TEXT NOT NULL,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    attribution_url TEXT,
    coverage_note TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note)
VALUES
    ('tcgdex', 'pokemon', 'TCGdex', 'https://api.tcgdex.net/v2', 'https://www.tcgdex.net/', 'Community-maintained multilingual catalog; some historical cards have no image.'),
    ('scryfall', 'magic', 'Scryfall', 'https://api.scryfall.com', 'https://scryfall.com/docs/api', 'Community-maintained catalog of Magic printings; images may be delayed for new cards.'),
    ('optcgapi', 'one_piece', 'OPTCG API', 'https://optcgapi.com/api', 'https://optcgapi.com/documentation', 'English community catalog; completeness depends on a third-party maintainer.'),
    ('onepiece_official', 'one_piece', 'One Piece Card Game official catalog', 'https://en.onepiece-cardgame.com/cardlist/', 'https://en.onepiece-cardgame.com/cardlist/', 'Official English series catalog; ingested politely with cached full snapshots.'),
    ('lorcast', 'lorcana', 'Lorcast', 'https://api.lorcast.com/v0', 'https://lorcast.com/', 'Community-maintained Lorcana catalog; completeness depends on a third-party maintainer.'),
    ('lorcanajson', 'lorcana', 'LorcanaJSON', 'https://lorcanajson.org/files/current', 'https://lorcanajson.org/', 'Versioned community snapshot with decodable publisher-hosted JPEG references.'),
    ('ygoprodeck', 'yugioh', 'YGOPRODeck', 'https://db.ygoprodeck.com/api/v7', 'https://ygoprodeck.com/api-guide/', 'Artwork-to-set-printing relationships are not provided by the source.'),
    ('weiss_official', 'weiss_schwarz', 'Weiss Schwarz official catalog', 'https://ws-tcg.com/cardlist/', 'https://ws-tcg.com/cardlist/', 'Official catalog availability and historical English coverage require verification.')
ON CONFLICT (id) DO UPDATE SET
    game = EXCLUDED.game,
    name = EXCLUDED.name,
    base_url = EXCLUDED.base_url,
    attribution_url = EXCLUDED.attribution_url,
    coverage_note = EXCLUDED.coverage_note,
    updated_at = NOW();

CREATE TABLE IF NOT EXISTS catalog_source_state (
    source_id TEXT PRIMARY KEY REFERENCES catalog_sources(id) ON DELETE CASCADE,
    cursor TEXT,
    etag TEXT,
    last_modified TEXT,
    upstream_version TEXT,
    last_attempt_at TIMESTAMP WITH TIME ZONE,
    last_success_at TIMESTAMP WITH TIME ZONE,
    last_full_sync_at TIMESTAMP WITH TIME ZONE,
    next_sync_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_record_count BIGINT NOT NULL DEFAULT 0,
    lease_owner TEXT,
    lease_until TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

INSERT INTO catalog_source_state (source_id)
SELECT id FROM catalog_sources
ON CONFLICT (source_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS catalog_sync_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id TEXT NOT NULL REFERENCES catalog_sources(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('full', 'incremental')),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'not_modified')),
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMP WITH TIME ZONE,
    upstream_version TEXT,
    fetched_count BIGINT NOT NULL DEFAULT 0,
    inserted_count BIGINT NOT NULL DEFAULT 0,
    updated_count BIGINT NOT NULL DEFAULT 0,
    deactivated_count BIGINT NOT NULL DEFAULT 0,
    image_count BIGINT NOT NULL DEFAULT 0,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_catalog_sync_runs_source_started
    ON catalog_sync_runs(source_id, started_at DESC);

ALTER TABLE cards ADD COLUMN IF NOT EXISTS source_id TEXT REFERENCES catalog_sources(id);
ALTER TABLE cards ADD COLUMN IF NOT EXISTS source_card_id TEXT;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS set_code TEXT;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS collector_number TEXT;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS source_updated_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS catalog_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE cards ADD COLUMN IF NOT EXISTS last_seen_run_id UUID REFERENCES catalog_sync_runs(id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cards_catalog_source_identity
    ON cards(source_id, source_card_id, language)
    WHERE source_id IS NOT NULL AND source_card_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cards_catalog_lookup
    ON cards(game, language, catalog_active);

CREATE INDEX IF NOT EXISTS idx_cards_catalog_last_seen_run
    ON cards(last_seen_run_id)
    WHERE last_seen_run_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS catalog_printings (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES catalog_sources(id) ON DELETE CASCADE,
    source_printing_id TEXT NOT NULL,
    set_code TEXT,
    set_name TEXT NOT NULL,
    collector_number TEXT,
    rarity TEXT,
    language TEXT NOT NULL,
    variant TEXT NOT NULL DEFAULT 'Normal',
    released_at DATE,
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    catalog_active BOOLEAN NOT NULL DEFAULT TRUE,
    first_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_seen_run_id UUID REFERENCES catalog_sync_runs(id),
    UNIQUE(source_id, source_printing_id, language, variant)
);

CREATE INDEX IF NOT EXISTS idx_catalog_printings_card
    ON catalog_printings(card_id, catalog_active);

CREATE INDEX IF NOT EXISTS idx_catalog_printings_last_seen_run
    ON catalog_printings(last_seen_run_id)
    WHERE last_seen_run_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS card_images (
    id BIGSERIAL PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    source_image_id TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'front',
    remote_url TEXT NOT NULL,
    local_path TEXT,
    content_sha256 TEXT,
    remote_etag TEXT,
    remote_last_modified TEXT,
    mime_type TEXT,
    width INTEGER,
    height INTEGER,
    byte_size BIGINT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'unavailable')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP WITH TIME ZONE,
    lease_owner TEXT,
    lease_until TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    downloaded_at TIMESTAMP WITH TIME ZONE,
    first_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(card_id, source_image_id)
);

CREATE INDEX IF NOT EXISTS idx_card_images_work_queue
    ON card_images(status, next_attempt_at)
    WHERE status IN ('pending', 'failed', 'processing');

CREATE INDEX IF NOT EXISTS idx_card_images_content_sha256
    ON card_images(content_sha256)
    WHERE content_sha256 IS NOT NULL;

CREATE TABLE IF NOT EXISTS catalog_printing_images (
    printing_id TEXT NOT NULL REFERENCES catalog_printings(id) ON DELETE CASCADE,
    image_id BIGINT NOT NULL REFERENCES card_images(id) ON DELETE CASCADE,
    PRIMARY KEY (printing_id, image_id)
);

CREATE TABLE IF NOT EXISTS card_fingerprints (
    image_id BIGINT NOT NULL REFERENCES card_images(id) ON DELETE CASCADE,
    algorithm TEXT NOT NULL,
    algorithm_version SMALLINT NOT NULL,
    transform TEXT NOT NULL DEFAULT 'full',
    hash BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (image_id, algorithm, algorithm_version, transform)
);

CREATE INDEX IF NOT EXISTS idx_card_fingerprints_algorithm
    ON card_fingerprints(algorithm, algorithm_version, transform);
