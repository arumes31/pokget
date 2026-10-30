-- Migration: Remove superseded catalog sources (optcgapi, lorcast) and add Star Wars: Unlimited (swudb)
-- Created At: 2026-10-30

-- 1. Remove superseded catalog sources
DELETE FROM catalog_sources WHERE id IN ('optcgapi', 'lorcast');

-- 2. Add Star Wars: Unlimited catalog source
INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES (
    'swudb',
    'star_wars_unlimited',
    'Star Wars: Unlimited DB',
    'https://api.swu-db.com',
    'https://swu-db.com',
    'Community-maintained complete Star Wars: Unlimited catalog.',
    true
)
ON CONFLICT (id) DO UPDATE SET
    game = EXCLUDED.game,
    name = EXCLUDED.name,
    base_url = EXCLUDED.base_url,
    attribution_url = EXCLUDED.attribution_url,
    coverage_note = EXCLUDED.coverage_note,
    enabled = EXCLUDED.enabled;
