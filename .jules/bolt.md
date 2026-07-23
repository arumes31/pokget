## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-07-23 - Optimize loop rate limiting
**Learning:** Using time.Sleep or reinitializing time.Ticker inside a loop blocks the entire iteration, preventing network/processing time from overlapping with the rate limit window.
**Action:** Initialize time.Ticker outside the loop and block on `<-ticker.C` inside the loop to allow overlapping execution and maximize throughput.
