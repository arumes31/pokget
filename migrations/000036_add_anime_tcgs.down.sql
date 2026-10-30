-- Migration Down: Remove Anime & Crossover TCGs
-- Created At: 2026-10-30

DELETE FROM catalog_source_state WHERE source_id IN (
    'fftcg',
    'shadowverse_evolve',
    'godzilla',
    'universus',
    'cardfight_vanguard',
    'hololive',
    'duel_masters',
    'cookierun_braverse',
    'force_of_will',
    'wixoss',
    'alpha_clash',
    'riftbound'
);

DELETE FROM catalog_sources WHERE id IN (
    'fftcg',
    'shadowverse_evolve',
    'godzilla',
    'universus',
    'cardfight_vanguard',
    'hololive',
    'duel_masters',
    'cookierun_braverse',
    'force_of_will',
    'wixoss',
    'alpha_clash',
    'riftbound'
);
