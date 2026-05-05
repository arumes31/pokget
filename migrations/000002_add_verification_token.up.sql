-- Add verification_token column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS verification_token TEXT;
