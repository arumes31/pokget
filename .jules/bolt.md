# Performance Journal - Bolt

## Price Sync Worker N+1 Query
- **Issue**: The worker was querying `price_alerts` for every single card in the database during each sync cycle.
- **Root Cause**: Classic N+1 anti-pattern. Query was inside the main cards loop.
- **Anti-pattern found**: `defer rows.Close()` inside a loop. This would cause memory pressure/file descriptor exhaustion if the loop had many iterations, as the rows wouldn't close until the function exited.
- **Optimization**: Batch-fetching all active alerts into a memory map (`map[card_id][]alert`) at the start of the sync cycle.
- **Impact**:
    - Reduced database roundtrips from O(N) to O(1) for alert checks.
    - Fixed potential resource leak by removing per-iteration `defer`.
    - Significantly improved sync speed for large catalogs.
