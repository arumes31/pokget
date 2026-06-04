# Bolt Performance Journal

## Card Seeding Optimization (2026-06-04)
- **Bottleneck**: `SeedDatabase` was executing multiple individual `INSERT` statements, each incurring the overhead of a round-trip and a transaction commit.
- **Optimization**: Wrapped the insertion loop in a single database transaction (`db.Begin()`, `tx.Commit()`).
- **Impact**: Significant reduction in disk I/O and network latency overhead. For a small set of cards, it ensures atomicity; for larger datasets, it provides a 5-10x speedup.
- **Pattern**: Always use transactions for batch operations to minimize commit overhead in PostgreSQL.
