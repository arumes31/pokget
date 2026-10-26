-- Stored hashes stay in place: the previous code resends the stored value as
-- the link token and compares it verbatim, so pending users can still verify.
ALTER TABLE users
    DROP COLUMN IF EXISTS verification_expires_at;
