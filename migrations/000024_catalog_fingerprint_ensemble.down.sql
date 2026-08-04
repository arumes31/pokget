DELETE FROM card_fingerprints
WHERE algorithm = 'phash64'
  AND algorithm_version = 1
  AND transform IN ('rotate_-3', 'rotate_+3');
