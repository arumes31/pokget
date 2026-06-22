## 2026-06-22 - Batch N+1 Database Query in Worker
**What:** The `checkPriceAlerts` function inside `syncPrices` was being called individually for each updated card, executing an N+1 `SELECT` query pattern against the `price_alerts` table.
**Why:** Running independent database queries inside a loop significantly degrades performance and blocks the application thread.
**Impact:** Eliminating the N+1 queries by collecting the cards and fetching the alerts in a single batched query cut the benchmark overhead drastically.
**Measurement:** Established benchmark decreased from `1507 ops, 740184 ns/op` to `2404 ops, 485438 ns/op`, roughly an 35% speedup.
