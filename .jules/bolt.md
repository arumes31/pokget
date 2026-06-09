## 2024-05-15 - Slice Allocation Bottleneck
**Learning:** Iterating database rows and appending to a nil slice (`var slice []Type`) is a common performance bottleneck in this app due to frequent small allocations.
**Action:** Use `make([]Type, 0, capacity)` to pre-allocate slices based on expected row counts (e.g. 8 for small sets, 64 for larger sets) to minimize GC pressure and re-allocations.
