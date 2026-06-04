# Performance Journal

## 2026-06-04 - Binary Search for Rank Lookups
- **Context:** User ranks were being looked up using linear search O(N).
- **Optimization:** Replaced linear loops with `sort.Search` (binary search) for O(log N) performance.
- **Impact:** While the number of ranks is small (10), binary search is more efficient and follows best practices for ordered datasets.
- **Learning:** Go's `sort.Search` is a powerful tool for optimizing lookups in sorted slices, even for small N, as it clearly communicates the "sorted" nature of the data.
