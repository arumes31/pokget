-- Migration: Gamification, Social Sharing, Graded Cards, and Wantlists
-- Created At: 2026-05-06

-- 1. Add gamification and avatar to users
ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 0;
ALTER TABLE users ADD COLUMN rank_title TEXT DEFAULT 'Novice Collector';
ALTER TABLE users ADD COLUMN avatar_url TEXT;

-- 2. Add graded card fields and visibility to portfolio
ALTER TABLE portfolio ADD COLUMN grade TEXT;
ALTER TABLE portfolio ADD COLUMN grading_company TEXT;
ALTER TABLE portfolio ADD COLUMN notes TEXT;
ALTER TABLE portfolio ADD COLUMN is_public BOOLEAN DEFAULT FALSE;

-- 3. Create wantlist table
CREATE TABLE IF NOT EXISTS wantlist (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    card_id TEXT NOT NULL, -- Card ID from the API/Scraper
    target_price DECIMAL(12, 2),
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 4. Create badges system
CREATE TABLE IF NOT EXISTS badges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    icon_url TEXT,
    xp_reward INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS user_badges (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    badge_id UUID REFERENCES badges(id) ON DELETE CASCADE,
    earned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, badge_id)
);

-- 5. Seed some initial badges
INSERT INTO badges (name, description, icon_url, xp_reward) VALUES
('First Pull', 'Add your first card to the portfolio', '/static/img/badges/first_pull.png', 50),
('Set Completionist', 'Complete a full master set', '/static/img/badges/completionist.png', 500),
('High Roller', 'Portfolio value exceeded $10,000', '/static/img/badges/high_roller.png', 1000),
('Early Adopter', 'Joined during the Pokget Beta', '/static/img/badges/beta.png', 200)
ON CONFLICT DO NOTHING;

-- 6. Add indexes for performance
CREATE INDEX IF NOT EXISTS idx_wantlist_user ON wantlist(user_id);
CREATE INDEX IF NOT EXISTS idx_portfolio_public ON portfolio(user_id, is_public);
