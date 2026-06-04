# Performance Journal

## 2026-06-04: Levenshtein Distance Optimization

### Context
The `levenshtein` function is used frequently during OCR scanning to match detected text against a database of card names. The original implementation used an O(N*M) space complexity matrix.

### Optimization
Replaced the matrix-based implementation with a space-optimized version that uses only one row (O(min(N,M)) space).

### Impact
- Significant reduction in memory allocation per OCR scan.
- Lower GC pressure.
