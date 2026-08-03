## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.

## 2026-06-08 - JSON Decoding Overhead
**Learning:** Decoding JSON from an `io.Reader` (like an `http.Response.Body`) by first reading all bytes into memory via `io.ReadAll` and then using `json.Unmarshal` allocates an unnecessarily large byte slice.
**Action:** Prefer streaming the response directly into the decoder using `json.NewDecoder(reader).Decode(&dest)`. When using this pattern with HTTP responses, explicitly drain the remaining body (e.g., `_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
