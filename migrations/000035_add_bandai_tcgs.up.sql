-- Migration: Add Bandai TCGs (Dragon Ball Super, Fusion World, Union Arena, Gundam, Battle Spirits Saga)
-- Created At: 2026-10-30

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    (
        'dbs_official',
        'dragon_ball_super',
        'Dragon Ball Super Card Game Official',
        'https://www.dbs-cardgame.com/us-en/cardlist',
        'https://www.dbs-cardgame.com/us-en/cardlist/',
        'Official Bandai Dragon Ball Super Card Game (Masters) catalog.',
        true
    ),
    (
        'dbs_fusion_world_official',
        'dbs_fusion_world',
        'Dragon Ball Super Fusion World Official',
        'https://www.dbs-cardgame.com/fw/en/cardlist',
        'https://www.dbs-cardgame.com/fw/en/cardlist/',
        'Official Bandai Dragon Ball Super Fusion World card database.',
        true
    ),
    (
        'unionarena_official',
        'union_arena',
        'Union Arena Official',
        'https://www.unionarena-tcg.com/na/cardlist',
        'https://www.unionarena-tcg.com/na/cardlist/',
        'Official Bandai Union Arena English card database.',
        true
    ),
    (
        'gundam_official',
        'gundam',
        'Gundam Card Game Official',
        'https://www.gundam-gcg.com/en/cards',
        'https://www.gundam-gcg.com/en/cards/',
        'Official Bandai Gundam Card Game database.',
        true
    ),
    (
        'battlespirits_official',
        'battlespirits_saga',
        'Battle Spirits Saga Official',
        'https://battlespirits-saga.com/cards',
        'https://battlespirits-saga.com/cards/',
        'Official Bandai Battle Spirits Saga card database.',
        true
    )
ON CONFLICT (id) DO UPDATE SET
    game = EXCLUDED.game,
    name = EXCLUDED.name,
    base_url = EXCLUDED.base_url,
    attribution_url = EXCLUDED.attribution_url,
    coverage_note = EXCLUDED.coverage_note,
    enabled = EXCLUDED.enabled;

INSERT INTO catalog_source_state (source_id)
VALUES
    ('dbs_official'),
    ('dbs_fusion_world_official'),
    ('unionarena_official'),
    ('gundam_official'),
    ('battlespirits_official')
ON CONFLICT (source_id) DO NOTHING;
