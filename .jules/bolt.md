## 2026-06-22 - Batch Database Insert for Price History
**What:** Modified `price_sync.go`'s `syncPrices` method to collect `price_history` updates into a slice and bulk insert them into PostgreSQL after the main loop, rather than inserting row-by-row (`N+1` queries).
**Why:** The original code exhibited an N+1 query pattern where an `INSERT` was executed inside a `for rows.Next()` loop. Batching significantly reduces network roundtrips to the DB and transaction overhead.
**Impact:** A local benchmark showed improvement from ~133ms/op to ~77ms/op (approx ~42% faster) when syncing a simulated batch of 100 cards.
