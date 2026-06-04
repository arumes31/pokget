## 2024-05-18 - Levenshtein Distance Optimization
**Learning:** In text/OCR applications that heavily rely on Levenshtein distance computations for fuzzy matching, using the standard O(N*M) matrix algorithm incurs enormous GC pressure and heap allocations.
**Action:** When computing Levenshtein distance, always apply the two-row spatial optimization to reduce memory overhead to O(min(N,M)) and improve overall cache locality.
