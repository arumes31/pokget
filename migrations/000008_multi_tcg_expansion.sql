-- Migration: Multi-TCG Expansion and Error Card Database
-- Created At: 2026-05-06

-- 1. Update cards table check constraint (if any) or just ensure data is clean
-- Note: existing 'game' column in 'cards' is TEXT, so we can just add data.

-- 2. Create error_cards table for community-driven tracking
CREATE TABLE IF NOT EXISTS error_cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    error_type TEXT NOT NULL, -- e.g., 'Holofoil Shift', 'Miscut', 'Ink Blot'
    description TEXT,
    estimated_value_multiplier DECIMAL(5, 2) DEFAULT 1.0,
    submitted_by UUID REFERENCES users(id),
    image_url TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. Add custom_price to portfolio for overrides
ALTER TABLE portfolio ADD COLUMN custom_price DECIMAL(12, 2);

-- 4. Add index for quick lookup
CREATE INDEX IF NOT EXISTS idx_error_cards_lookup ON error_cards(card_id);

-- 4. Seed some initial Lorcana/MTG/Weiss data if needed (usually handled by scraper)
-- But we can add the 'game' types to a reference table if we had one.
-- For now, we'll just allow these strings in the 'game' column.
