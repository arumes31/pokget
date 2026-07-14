## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2025-03-01 - Optimize JSON decoding
**Learning:** Using `json.NewDecoder(resp.Body).Decode(&dest)` instead of `io.ReadAll` and `json.Unmarshal` avoids large byte slice allocations and improves performance when processing HTTP responses. When doing this, explicitly draining the body (`io.Copy(io.Discard, resp.Body)`) is critical to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
**Action:** When working with JSON HTTP responses, always prefer `json.NewDecoder` and ensure the body is drained after decoding to maximize keep-alive reuse.
