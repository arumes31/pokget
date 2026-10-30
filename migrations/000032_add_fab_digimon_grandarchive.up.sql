-- Migration: Add Flesh and Blood (fabcube), Digimon Card Game (digimoncardio), and Grand Archive TCG (gatcg)
-- Created At: 2026-10-30

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    (
        'fabcube',
        'flesh_and_blood',
        'The FAB Cube',
        'https://raw.githubusercontent.com/the-fab-cube/flesh-and-blood-cards/refs/heads/main',
        'https://thefabcube.com',
        'Community-maintained Flesh and Blood card database.',
        true
    ),
    (
        'digimoncardio',
        'digimon',
        'DigimonCard.io',
        'https://digimoncard.io/api-public',
        'https://digimoncard.io',
        'Community-maintained Digimon Card Game database.',
        true
    ),
    (
        'gatcg',
        'grand_archive',
        'Grand Archive Index',
        'https://api.gatcg.com',
        'https://gatcg.com',
        'Official Grand Archive TCG developer API.',
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
    ('fabcube'),
    ('digimoncardio'),
    ('gatcg')
ON CONFLICT (source_id) DO NOTHING;
