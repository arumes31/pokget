## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-08-01 - JSON Decode Overhead on Large HTTP Responses
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` on HTTP responses creates an unnecessary full-sized byte slice allocation on the heap, increasing GC pressure for large LLM conversational payloads.
**Action:** Always prefer `json.NewDecoder(resp.Body).Decode(&dest)` for HTTP responses, and remember to explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) to ensure the underlying TCP connection can be reused by the HTTP transport (Keep-Alive).
