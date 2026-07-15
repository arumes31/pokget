## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-08 - JSON Decoding Overhead with io.ReadAll
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` allocates an unnecessarily large byte slice for HTTP response bodies. Using `json.NewDecoder(resp.Body).Decode(&dest)` avoids this allocation.
**Action:** When reading JSON from an `io.Reader` (like an `http.Response.Body`), prefer `json.NewDecoder`. Also, explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
