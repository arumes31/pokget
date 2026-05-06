-- Migration: Resilience Fixes
-- Created At: 2026-05-06

-- 1. Ensure audit_logs exists (it might have been missed due to dirty migration forcing)
CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    user_id TEXT,
    action TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 2. Ensure payload column exists in audit_logs
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_logs' AND column_name='payload') THEN
        ALTER TABLE audit_logs ADD COLUMN payload JSONB;
    END IF;
END $$;

-- 3. Ensure cards.game column exists
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='cards' AND column_name='game') THEN
        ALTER TABLE cards ADD COLUMN game TEXT DEFAULT 'pokemon';
    END IF;
END $$;

-- 4. Ensure other critical columns from improvements batch exist
ALTER TABLE portfolio ADD COLUMN IF NOT EXISTS language TEXT DEFAULT 'en';
ALTER TABLE users ADD COLUMN IF NOT EXISTS condition_multipliers JSONB DEFAULT '{"NM": 1.0, "LP": 0.9, "MP": 0.7, "HP": 0.5, "DMG": 0.3}';
ALTER TABLE users ADD COLUMN IF NOT EXISTS public_slug TEXT UNIQUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_public_profile BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE;
