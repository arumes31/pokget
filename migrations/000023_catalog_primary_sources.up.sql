INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note)
VALUES
    ('onepiece_official', 'one_piece', 'One Piece Card Game official catalog', 'https://en.onepiece-cardgame.com/cardlist/', 'https://en.onepiece-cardgame.com/cardlist/', 'Official English series catalog; ingested politely with cached full snapshots.'),
    ('lorcanajson', 'lorcana', 'LorcanaJSON', 'https://lorcanajson.org/files/current', 'https://lorcanajson.org/', 'Versioned community snapshot with decodable publisher-hosted JPEG references.')
ON CONFLICT (id) DO UPDATE SET
    game = EXCLUDED.game,
    name = EXCLUDED.name,
    base_url = EXCLUDED.base_url,
    attribution_url = EXCLUDED.attribution_url,
    coverage_note = EXCLUDED.coverage_note,
    enabled = TRUE,
    updated_at = NOW();

INSERT INTO catalog_source_state (source_id)
VALUES ('onepiece_official'), ('lorcanajson')
ON CONFLICT (source_id) DO NOTHING;
