# Bolt Performance Journal - Pokget Vault

## Optimization: Binder List Card Counting
- **Date**: 2026-06-04
- **Pattern**: Correlated subquery for counting related entities.
- **Context**: `internal/service/binder.go` in `GetBindersByUserID`.
- **Finding**: Using `LEFT JOIN` with `GROUP BY` can lead to performance degradation as the portfolio table grows. The join happens before the group by, potentially creating a large intermediate result set.
- **Solution**: Replaced `LEFT JOIN` + `GROUP BY` with a correlated subquery: `(SELECT COUNT(*) FROM portfolio WHERE binder_id = binders.id)`.
- **Expected Impact**: O(B * log(P)) where B is number of binders and P is total portfolio items (assuming index on binder_id), compared to O(B+P) for a join. For typical users with few binders but many cards, this is significantly more efficient and avoids memory-intensive join operations.
