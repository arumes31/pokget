-- Migration Down: Remove Flesh and Blood (fabcube), Digimon Card Game (digimoncardio), and Grand Archive TCG (gatcg)

DELETE FROM catalog_source_state WHERE source_id IN ('fabcube', 'digimoncardio', 'gatcg');
DELETE FROM catalog_sources WHERE id IN ('fabcube', 'digimoncardio', 'gatcg');
