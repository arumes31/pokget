## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-07-28 - Rate Limiting Overlap via time.Ticker
**Learning:** When rate limiting a loop containing synchronous operations (like network requests), placing `time.Sleep()` inside the loop forces sequential processing (`wait + process`). This acts as a fixed delay rather than a true rate limiter and significantly reduces throughput.
**Action:** Initialize a `time.Ticker` before the loop and block on `<-ticker.C` at the start of each iteration. This allows the operation's processing time to overlap with the rate limit window, correctly bounding throughput to the intended rate without unnecessary idle waiting.
