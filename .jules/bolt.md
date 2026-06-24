## 2026-06-24 - Redundant Global Data Fetching in Handlers
**Learning:** The base render function automatically fetches and injects global user data (XP, Rank, etc.) into the template context. Manually fetching this same data inside individual handlers like `Dashboard` causes an unnecessary redundant database query.
**Action:** Use the global variables injected by `render` (e.g., `UserXP`, `UserRank`) in templates and remove redundant DB fetches from individual handlers to prevent N+1 style query bottlenecks.
