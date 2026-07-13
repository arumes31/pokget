## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-15 - Ticker Overlap in Rate Limiting
**Learning:** Using time.Sleep() or initializing a new time.Ticker inside a processing loop causes sequential blocking, meaning processing time adds up with the wait time.
**Action:** Always initialize a single time.Ticker before the loop and block on <-ticker.C inside the loop. This allows processing time to overlap with the rate limit window, preventing sequential blocking delays and improving overall throughput.
