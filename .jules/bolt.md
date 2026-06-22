
## 2024-05-18 - Removed Redundant DB Query for XP/Rank in Dashboard
**Learning:** Found a performance pattern where a specific handler (`Dashboard`) manually queried the DB for user XP and Rank, performing Gamification calculations, and explicitly passing these to the template. However, the base `h.render` function already performs this exact query and injects this data into the global template map context (`UserXP`, `UserRank`, etc.) for every authenticated route.
**Action:** Always check the base `render` or middleware functions to see what global data is already being injected into templates, preventing redundant DB queries in individual handlers.
