## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-05-19 - Stream JSON decoding for HTTP responses
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` for HTTP response bodies forces the entire payload into a large byte slice allocation. Replacing this with `json.NewDecoder(resp.Body).Decode` reduces memory overhead, but requires explicitly draining the remaining body (e.g., `io.Copy(io.Discard, resp.Body)`) to ensure the HTTP transport can reuse the TCP connection for Keep-Alive.
**Action:** Always prefer `json.NewDecoder` for stream-based inputs, and remember to drain the connection if HTTP Keep-Alive is desired.
