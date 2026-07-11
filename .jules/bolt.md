## 2026-06-08 - String Split Overhead in Middlewares
**Learning:** `strings.Split` causes significant heap allocations (allocating a slice of strings) which is particularly detrimental inside high-frequency middleware like `ProxyMiddleware`.
**Action:** Always prefer `strings.IndexByte` and manual string slicing when extracting a specific segment from a character-delimited string (like headers or IPs) to eliminate unnecessary garbage collection pressure in hot paths.
## 2024-05-18 - Avoid unnecessary memory allocation using JSON streaming
**Learning:** When decoding JSON from an io.Reader (like an http.Response.Body), prefer json.NewDecoder(reader).Decode(&dest) over io.ReadAll(reader) followed by json.Unmarshal() to avoid allocating an unnecessary large byte slice. Crucially, when using this with HTTP responses, explicitly drain the remaining body (`_, _ = io.Copy(io.Discard, resp.Body)`) after decoding to ensure the HTTP transport can reuse the TCP connection (Keep-Alive).
**Action:** Use json.NewDecoder for any HTTP request/response JSON decoding instead of buffering to memory, and remember to drain the body.
