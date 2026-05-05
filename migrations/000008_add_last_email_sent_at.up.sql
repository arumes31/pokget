-- Add last_email_sent_at column to users table for rate limiting email resends
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_email_sent_at TIMESTAMP WITH TIME ZONE;
