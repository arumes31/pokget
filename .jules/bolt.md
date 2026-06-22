## 2026-06-22 - Batch Database Updates
**What:** Modified `syncPrices()` in `internal/worker/price_sync.go` to wrap the loop updating card prices into a single database transaction and to use prepared statements for the `UPDATE` and `INSERT` queries instead of calling `Exec` with string queries every loop iteration.
**Why:** The original code had a classic N+1 query issue, calling `.Exec` on `UPDATE cards` and `INSERT INTO price_history` inside a `for rows.Next()` loop. This caused significant database round trips and allocation overhead parsing identical query strings.
**Impact:** Reduced operation time per 100 cards from ~7.98ms down to ~0.065ms.
**Measurement:** Added benchmark `BenchmarkSyncPrices` measuring memory allocations dropping from ~19,160 allocs/op to just 75 allocs/op, significantly reducing CPU work and GC pressure.
