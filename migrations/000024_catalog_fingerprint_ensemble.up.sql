UPDATE card_images AS image
SET status = 'pending', next_attempt_at = NULL, lease_owner = NULL,
    lease_until = NULL, last_error = NULL
WHERE image.status = 'ready'
  AND NOT EXISTS (
      SELECT 1
      FROM card_fingerprints AS fingerprint
      WHERE fingerprint.image_id = image.id
        AND fingerprint.algorithm = 'phash64'
        AND fingerprint.algorithm_version = 1
        AND fingerprint.transform = 'rotate_+3'
  );
