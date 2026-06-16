## 2026-06-16 - [Proxy Middleware IP Parsing Allocation Reduction]
**Learning:** Using `strings.Split` for parsing comma-separated HTTP headers like `X-Forwarded-For` inside a middleware allocates memory on every request.
**Action:** Replace `strings.Split` with `strings.IndexByte` to extract the first element of a comma-separated list without allocating new slice arrays, improving latency and reducing GC pressure on high-traffic routes.
