## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2024-07-04 - Avoid large allocations when decoding HTTP response bodies
**Learning:** `io.ReadAll` followed by `json.Unmarshal` allocates a large byte slice in memory to hold the entire HTTP response body before parsing it. This causes unnecessary memory allocations and increases garbage collection (GC) pressure, especially when the response is large.
**Action:** Use `json.NewDecoder(resp.Body).Decode(&dest)` instead to stream the HTTP response body directly into the JSON decoder, avoiding the large intermediate byte slice allocation.
