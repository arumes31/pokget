## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-06-08 - JSON Streaming Decode over ReadAll
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` for HTTP responses forces the application to allocate a contiguous byte slice large enough to hold the entire body before parsing begins. This is highly inefficient for memory, especially with potentially large LLM API responses.
**Action:** Always prefer `json.NewDecoder(resp.Body).Decode(&target)`. Crucially, when applying this to HTTP responses, follow the decode with `_, _ = io.Copy(io.Discard, resp.Body)` to ensure any remaining unread bytes are drained, allowing the HTTP transport to reuse the TCP connection (Keep-Alive).
