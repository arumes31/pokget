# Performance Journal

## 2026-06-04: PublicVault Optimization
- **What**: Replaced `strings.Split(email, "@")[0]` with `strings.IndexByte` and slicing.
- **Why**: `strings.Split` allocates a new slice and multiple strings, whereas `IndexByte` + slicing avoids the slice allocation and uses the existing underlying string array.
- **Impact**: Minor reduction in heap allocations per `PublicVault` request.
