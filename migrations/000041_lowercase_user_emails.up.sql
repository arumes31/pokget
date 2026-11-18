-- Canonicalize existing user emails to lowercase so they match the lowercased
-- form that registration and login now store and query.
--
-- Only rows WITHOUT surrounding whitespace are touched (email = btrim(email)):
-- the handlers' canonical lookup key is trimmed+lowercased, so lowercasing a
-- space-padded row would produce a value that neither the exact-submitted nor
-- the canonical lookup argument can match, orphaning the account. Padded rows
-- are therefore left as-is and stay reachable by their exact submitted spelling.
--
-- Rows that already have a case-insensitive twin are skipped so this can never
-- violate the UNIQUE constraint and leave a half-applied migration at startup.
-- Idempotent: a second run matches no rows.
UPDATE users u
SET email = LOWER(u.email)
WHERE u.email <> LOWER(u.email)
  AND u.email = btrim(u.email)
  AND NOT EXISTS (
    SELECT 1 FROM users o
    WHERE o.id <> u.id
      AND LOWER(o.email) = LOWER(u.email)
  );
