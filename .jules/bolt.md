## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-07-12 - JSON Decoding Overhead
**Learning:** Decoding JSON from an io.Reader (like an http.Response.Body) using io.ReadAll followed by json.Unmarshal unnecessarily allocates a large byte slice.
**Action:** Prefer json.NewDecoder(reader).Decode(&dest) over io.ReadAll. Crucially, when using this with HTTP responses, explicitly drain the remaining body (_, _ = io.Copy(io.Discard, resp.Body)) to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
