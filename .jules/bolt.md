# Performance Journal - Bolt

## sync.Map for Rate Limiting
- **Context**: `internal/auth/auth.go` used a global map and a `sync.Mutex` for tracking IP-based rate limiters.
- **Learning**: Under high-concurrency (e.g., login brute-force attempts or many users logging in at once), the global mutex becomes a bottleneck.
- **Optimization**: Swapping to `sync.Map` reduces lock contention because `sync.Map` is optimized for cases where keys are stable and reads outnumber writes (once a rate limiter for an IP is created, it is mostly read).
- **Measurement**: Expected to improve throughput of the `RateLimitMiddleware` by reducing goroutine waiting time on the mutex.
