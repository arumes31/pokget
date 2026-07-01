## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-07-01 - JSON Decoding Overhead
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` to parse HTTP responses causes unnecessary allocations for large byte slices.
**Action:** Prefer `json.NewDecoder(resp.Body).Decode(&dest)` to stream and decode the JSON response directly, reducing memory overhead.
