-- Migration Down: Remove Bandai TCGs
-- Created At: 2026-10-30

DELETE FROM catalog_source_state WHERE source_id IN ('dbs_official', 'dbs_fusion_world_official', 'unionarena_official', 'gundam_official', 'battlespirits_official');
DELETE FROM catalog_sources WHERE id IN ('dbs_official', 'dbs_fusion_world_official', 'unionarena_official', 'gundam_official', 'battlespirits_official');
