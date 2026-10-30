-- Migration Down: Remove Altered TCG (altered) and Sorcery: Contested Realm (sorcery)

DELETE FROM catalog_source_state WHERE source_id IN ('altered', 'sorcery');
DELETE FROM catalog_sources WHERE id IN ('altered', 'sorcery');
