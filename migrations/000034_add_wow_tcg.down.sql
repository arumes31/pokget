-- Migration Down: Remove World of Warcraft TCG (wowtcg)

DELETE FROM catalog_source_state WHERE source_id = 'wowtcg';
DELETE FROM catalog_sources WHERE id = 'wowtcg';
