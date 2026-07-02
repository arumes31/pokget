## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-06-08 - JSON Decoding Allocations
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` for HTTP responses allocates a large intermediate byte slice, wasting memory and increasing GC pressure, especially for potentially large LLM payloads.
**Action:** Prefer `json.NewDecoder(resp.Body).Decode(&dest)` to decode directly from the stream.
