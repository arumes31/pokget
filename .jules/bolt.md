## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-08 - Redundant Database Queries in Handlers
**Learning:** Specific HTTP handlers (like `Dashboard`) may redundantly query global user data (like XP or Rank) and explicitly pass it into template maps, even though the base `render` method already executes this query and injects the same data into the template context under standardized keys (`UserXP`, `UserRank`). This causes unnecessary `SELECT` queries on page load.
**Action:** Always verify what the global `render` function or template base context automatically injects before fetching database records for templates.
