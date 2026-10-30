-- Migration: Add Altered TCG (altered) and Sorcery: Contested Realm (sorcery)
-- Created At: 2026-10-30

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    (
        'altered',
        'altered',
        'Altered TCG Card Database',
        'https://raw.githubusercontent.com/PolluxTroy0/Altered-TCG-Card-Database/main',
        'https://altered.gg',
        'Community-maintained complete Altered TCG catalog with multilingual support.',
        true
    ),
    (
        'sorcery',
        'sorcery',
        'Sorcery: Contested Realm',
        'https://api.sorcerytcg.com',
        'https://sorcerytcg.com',
        'Official Sorcery: Contested Realm public card API.',
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
    ('altered'),
    ('sorcery')
ON CONFLICT (source_id) DO NOTHING;
