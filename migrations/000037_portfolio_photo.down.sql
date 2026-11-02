-- Migration Down: Remove the stored card photo reference
-- Created At: 2026-11-02

ALTER TABLE portfolio DROP COLUMN IF EXISTS photo_updated_at;
ALTER TABLE portfolio DROP COLUMN IF EXISTS photo_path;
