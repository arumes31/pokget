
## [2026-06-04] Perceptual Hash Matching Optimization

- **Issue:** `MatchFingerprint` was allocating `goimagehash.ImageHash` objects for every card in the database during matching, leading to O(N) heap allocations.
- **Optimization:** Replaced object-oriented distance calculation with direct bitwise XOR and `math/bits.OnesCount64`.
- **Impact:** Eliminated all heap allocations in the matching loop. Benchmark showed reduction from 176 KB/op and 1000 allocs/op to 0 B/op and 0 allocs/op for 1000 cards.
