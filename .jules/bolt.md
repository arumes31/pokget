## 2023-10-27 - Rate Limiter sync.Map Optimization
**Learning:** Using a standard `map` with a `sync.Mutex` in a high-throughput middleware like the rate limiter causes lock contention across all requests, creating a bottleneck.
**Action:** Replace `map` + `sync.Mutex` with `sync.Map` for mostly-read concurrent maps to eliminate locking overhead on cache hits.
