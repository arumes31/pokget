# Performance Journal

## 2026-06-04: sync.Map for Rate Limiters
- **Context**: The `internal/auth` package uses a global mutex to protect a map of rate limiters.
- **Optimization**: Replaced `map` + `sync.Mutex` with `sync.Map`.
- **Reasoning**: `sync.Map` is optimized for cases where keys are mostly read or updated independently, reducing lock contention on a single global mutex during concurrent requests from different IPs.
- **Impact**: Better scalability under high traffic for the rate limiting middleware.
