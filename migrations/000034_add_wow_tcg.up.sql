-- Migration: Add World of Warcraft TCG (wowtcg)
-- Created At: 2026-10-30

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    (
        'wowtcg',
        'world_of_warcraft',
        'WoW TCG Browser',
        'https://wow.tcgbrowser.com',
        'https://wow.tcgbrowser.com',
        'Complete database of World of Warcraft Trading Card Game cards and MMO loot cards.',
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
    ('wowtcg')
ON CONFLICT (source_id) DO NOTHING;
