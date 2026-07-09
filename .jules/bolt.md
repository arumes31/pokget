## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-06-08 - JSON Decoding Overhead
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` reads the entire payload into a large byte slice before processing, leading to unnecessary memory allocations, which is especially problematic for large API responses like those from LLMs.
**Action:** Use `json.NewDecoder(io.Reader).Decode(&dest)` to stream and parse JSON directly from the reader, avoiding large intermediate byte slice allocations.
