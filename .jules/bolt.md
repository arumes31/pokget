## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-08 - Streaming JSON Decoding and Connection Reuse
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` for HTTP responses causes unnecessary large byte slice allocations. Furthermore, when using `json.NewDecoder`, failing to drain the remaining response body prevents the HTTP transport from reusing the TCP connection (Keep-Alive).
**Action:** Always prefer `json.NewDecoder(resp.Body).Decode(&dest)` for HTTP responses and explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure connection reuse.
