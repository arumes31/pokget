## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-07-18 - Ticker over Sleep in rate-limited loops
**Learning:** Using time.Sleep() inside processing loops introduces sequential blocking delays that reduce throughput. Initializing a time.Ticker before the loop and blocking on <-ticker.C allows the actual processing time (DB interactions, API calls) to overlap with the rate limiting window.
**Action:** Use time.Ticker instead of time.Sleep for rate limiting inside processing loops to improve overall throughput.
