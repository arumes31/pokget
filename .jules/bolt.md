## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2026-06-08 - Optimize JSON Decoding
**Learning:** Using `io.ReadAll` followed by `json.Unmarshal` for HTTP responses allocates an unnecessarily large byte slice. Using `json.NewDecoder(resp.Body).Decode(&dest)` avoids this.
**Action:** Always prefer `json.NewDecoder` for `io.Reader` sources like HTTP responses. Crucially, follow it with `_, _ = io.Copy(io.Discard, resp.Body)` to drain the body so the HTTP transport can reuse the TCP connection (Keep-Alive).
