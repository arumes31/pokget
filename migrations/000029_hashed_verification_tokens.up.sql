ALTER TABLE users
    ADD COLUMN IF NOT EXISTS verification_expires_at TIMESTAMP WITH TIME ZONE;

-- Store only the SHA-256 of pending email verification tokens. Links that were
-- already sent keep working for another 24 hours. Rows that already have an
-- expiry hold a hash, so a re-applied migration does not hash them twice.
UPDATE users
SET verification_token = encode(sha256(convert_to(verification_token, 'UTF8')), 'hex'),
    verification_expires_at = NOW() + INTERVAL '24 hours'
WHERE verification_token IS NOT NULL
  AND verification_expires_at IS NULL;
