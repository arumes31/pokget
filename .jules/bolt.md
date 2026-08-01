## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2025-05-18 - JSON Decoding HTTP Responses
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` when parsing JSON from an `http.Response.Body` allocates a large, unnecessary byte slice, increasing memory pressure.
**Action:** Prefer `json.NewDecoder(resp.Body).Decode(&dest)`. Crucially, always explicitly drain the remaining response body (`_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
