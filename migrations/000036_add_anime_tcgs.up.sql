-- Migration: Add Anime & Crossover TCGs (FFTCG, Shadowverse: Evolve, Godzilla, UniVersus, Vanguard, hololive, Duel Masters, CookieRun)
-- Created At: 2026-10-30

INSERT INTO catalog_sources (id, game, name, base_url, attribution_url, coverage_note, enabled)
VALUES
    (
        'fftcg',
        'fftcg',
        'Final Fantasy TCG Official',
        'https://fftcg.square-enix-games.com',
        'https://fftcg.square-enix-games.com',
        'Official Square Enix Final Fantasy Trading Card Game card database with multi-language support (EN, JA, DE, ES, FR, IT).',
        true
    ),
    (
        'shadowverse_evolve',
        'shadowverse_evolve',
        'Shadowverse: Evolve Official',
        'https://en.shadowverse-evolve.com',
        'https://en.shadowverse-evolve.com/cards/',
        'Official Bushiroad & Cygames Shadowverse: Evolve card database.',
        true
    ),
    (
        'godzilla',
        'godzilla',
        'Godzilla Card Game Official',
        'https://en.godzilla-cardgame.com',
        'https://en.godzilla-cardgame.com/cardlist/',
        'Official Bushiroad & Toho Godzilla Card Game database.',
        true
    ),
    (
        'universus',
        'universus',
        'UVS Ultra',
        'https://uvsultra.online',
        'https://uvsultra.online',
        'Community and competitive UniVersus card database and deckbuilder.',
        true
    ),
    (
        'cardfight_vanguard',
        'vanguard',
        'Cardfight!! Vanguard Official',
        'https://en.cf-vanguard.com',
        'https://en.cf-vanguard.com/cardlist/',
        'Official Bushiroad Cardfight!! Vanguard card database.',
        true
    ),
    (
        'hololive',
        'hololive',
        'hololive Official Card Game',
        'https://hololive-official-cardgame.com',
        'https://hololive-official-cardgame.com/cardlist/',
        'Official Cover Corp & Bushiroad hololive Official Card Game database.',
        true
    ),
    (
        'duel_masters',
        'duel_masters',
        'Duel Masters Index',
        'https://raw.githubusercontent.com/Latepate64/duel-masters-json/master',
        'https://github.com/Latepate64/duel-masters-json',
        'Complete classic English Duel Masters card database.',
        true
    ),
    (
        'cookierun_braverse',
        'cookie_run',
        'CookieRun: Braverse (TCGCSV)',
        'https://tcgcsv.com/tcgplayer',
        'https://tcgcsv.com',
        'CookieRun: Braverse card catalog mirrored from TCGplayer.',
        true
    ),
    (
        'force_of_will',
        'force_of_will',
        'Force of Will (TCGCSV)',
        'https://tcgcsv.com/tcgplayer',
        'https://tcgcsv.com',
        'Force of Will card catalog mirrored from TCGplayer.',
        true
    ),
    (
        'wixoss',
        'wixoss',
        'WIXOSS (TCGCSV)',
        'https://tcgcsv.com/tcgplayer',
        'https://tcgcsv.com',
        'WIXOSS card catalog mirrored from TCGplayer.',
        true
    ),
    (
        'alpha_clash',
        'alpha_clash',
        'Alpha Clash (TCGCSV)',
        'https://tcgcsv.com/tcgplayer',
        'https://tcgcsv.com',
        'Alpha Clash card catalog mirrored from TCGplayer.',
        true
    ),
    (
        'riftbound',
        'riftbound',
        'Riftbound: League of Legends TCG (TCGCSV)',
        'https://tcgcsv.com/tcgplayer',
        'https://tcgcsv.com',
        'Riftbound: League of Legends TCG card catalog mirrored from TCGplayer.',
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
    ('fftcg'),
    ('shadowverse_evolve'),
    ('godzilla'),
    ('universus'),
    ('cardfight_vanguard'),
    ('hololive'),
    ('duel_masters'),
    ('cookierun_braverse'),
    ('force_of_will'),
    ('wixoss'),
    ('alpha_clash'),
    ('riftbound')
ON CONFLICT (source_id) DO NOTHING;
