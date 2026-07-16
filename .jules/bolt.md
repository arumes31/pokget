## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-08 - JSON Decoding for HTTP Responses
**Learning:** Using `io.ReadAll` to read an entire HTTP response body before unmarshaling it (`json.Unmarshal`) forces the allocation of a large byte slice containing the full payload in memory, increasing garbage collector overhead for large API responses.
**Action:** Always prefer stream decoding (`json.NewDecoder(resp.Body).Decode(&dest)`) for network requests. Crucially, explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure the Go HTTP client can reuse the underlying TCP connection (Keep-Alive), as `json.NewDecoder` stops reading once a valid JSON object is found.
