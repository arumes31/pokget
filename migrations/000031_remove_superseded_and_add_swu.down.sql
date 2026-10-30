-- Migration Down: Restore superseded catalog sources and remove Star Wars: Unlimited (swudb)

DELETE FROM catalog_sources WHERE id = 'swudb';

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    ('optcgapi', 'one_piece', 'OPTCG API', 'https://optcgapi.com/api', 'https://optcgapi.com/documentation', 'English community catalog; completeness depends on a third-party maintainer.', false),
    ('lorcast', 'lorcana', 'Lorcast', 'https://api.lorcast.com/v0', 'https://lorcast.com/', 'Community-maintained Lorcana catalog; completeness depends on a third-party maintainer.', false)
ON CONFLICT (id) DO NOTHING;
