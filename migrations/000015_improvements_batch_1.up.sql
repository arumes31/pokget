-- Migration: Improvements Batch 1
-- Created At: 2026-05-06

-- 0. Ensure base tables from previous broken migrations exist
CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    user_id TEXT,
    action TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='cards' AND column_name='game') THEN
        ALTER TABLE cards ADD COLUMN game TEXT DEFAULT 'pokemon';
    END IF;
END $$;

-- 1. Add language support to portfolio (instance specific)
ALTER TABLE portfolio ADD COLUMN IF NOT EXISTS language TEXT DEFAULT 'en';

-- 2. Add personalized condition multipliers to users
-- Example: {"NM": 1.0, "LP": 0.9, "MP": 0.7, "HP": 0.5, "DMG": 0.3}
ALTER TABLE users ADD COLUMN IF NOT EXISTS condition_multipliers JSONB DEFAULT '{"NM": 1.0, "LP": 0.9, "MP": 0.7, "HP": 0.5, "DMG": 0.3}';

-- 3. Add public profile support
ALTER TABLE users ADD COLUMN IF NOT EXISTS public_slug TEXT UNIQUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_public_profile BOOLEAN DEFAULT FALSE;

-- 4. Add MFA support
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE;

-- 5. Expand audit logs if needed (ensuring payload exists)
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_logs' AND column_name='payload') THEN
        ALTER TABLE audit_logs ADD COLUMN payload JSONB;
    END IF;
END $$;

-- 6. Index for public profile lookup
CREATE INDEX IF NOT EXISTS idx_users_public_slug ON users(public_slug) WHERE is_public_profile = TRUE;
