## 2025-05-18 - Pre-allocate slices during DB iteration
**Learning:** Initializing slices with `var slice []Type` and appending to them inside loops over database rows (e.g., `rows.Next()`) can cause multiple underlying array re-allocations and increase garbage collection pressure, particularly when dealing with lists that have a predictable average size (like portfolios or binders).
**Action:** Always pre-allocate slices with a sensible default capacity (e.g., `make([]Type, 0, 16)`) instead of declaring them as nil when appending inside loops over database rows.
