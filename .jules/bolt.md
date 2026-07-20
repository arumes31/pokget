## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2025-05-24 - Optimize JSON decoding from HTTP responses
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` when reading HTTP responses creates an unnecessarily large byte slice allocation, especially for large payloads.
**Action:** Prefer `json.NewDecoder(resp.Body).Decode(&dest)` to decode directly from the stream, and explicitly drain the remaining body `_, _ = io.Copy(io.Discard, resp.Body)` to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
