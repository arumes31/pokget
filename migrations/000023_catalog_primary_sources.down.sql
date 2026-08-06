DELETE FROM catalog_sources
WHERE id IN ('onepiece_official', 'lorcanajson')
  AND NOT EXISTS (SELECT 1 FROM cards WHERE cards.source_id = catalog_sources.id);
