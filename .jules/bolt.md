## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-07-22 - JSON Decoding Overhead with io.ReadAll
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` on an `http.Response.Body` allocates an unnecessary large byte slice. Furthermore, failing to explicitly drain the remaining body prevents the HTTP transport from reusing the TCP connection (Keep-Alive), causing performance degradation.
**Action:** Always prefer `json.NewDecoder(reader).Decode(&dest)` over `io.ReadAll` and explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) when working with HTTP responses.
