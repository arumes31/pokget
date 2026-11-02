-- Migration: Store the photo a collector took of their own card
-- Created At: 2026-11-02

ALTER TABLE portfolio ADD COLUMN IF NOT EXISTS photo_path TEXT;
ALTER TABLE portfolio ADD COLUMN IF NOT EXISTS photo_updated_at TIMESTAMP WITH TIME ZONE;
