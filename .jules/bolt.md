## 2024-05-24 - Pre-allocating slices to reduce GC pressure
**Learning:** Found multiple instances where slices were declared as `var slice []T` and then appended to within loops over database rows.
**Action:** Always prefer `slice := make([]T, 0, capacity)` when the approximate capacity is known to minimize underlying array re-allocations and reduce garbage collection pressure.
