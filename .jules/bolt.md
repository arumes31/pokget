# Bolt Performance Journal

## 2026-06-04: Card Indexing and Search
- **Learning**: The codebase relies heavily on card searches by name, especially during OCR fallbacks and visual matching.
- **Architectural Pattern**: `internal/handlers/wantlist.go` performs a JOIN with `cards` on `card_id`.
- **Optimization Strategy**: Indexes on `cards.name` and `portfolio.user_id` are already present in migration `000010_indexes.up.sql`. However, `wantlist.user_id` lacks an index, which will become a bottleneck as the table grows.
- **Action**: Identified need for index on `wantlist(user_id)` to speed up personal wantlist retrieval.

## 2026-06-04: HTMX Trigger Overhead
- **Learning**: `AddToWantlist` re-renders the entire wantlist after a successful insertion.
- **Impact**: While clean from a backend perspective, it causes a full list re-fetch and re-render.
- **Optimization Strategy**: For small wantlists, this is fine. For larger ones, OOB (Out-of-Band) swaps or returning just the new item might be more efficient.
