## 2023-11-20 - [Avoid string slice allocations in HTTP middleware]
**Learning:** In hot paths like HTTP middleware (e.g. `ProxyMiddleware` handling `X-Forwarded-For` headers), using `strings.Split` causes unnecessary heap allocations for the resulting string slice, which can add significant GC pressure when processing nearly every incoming request.
**Action:** Replace `strings.Split` with `strings.IndexByte` and string slicing whenever you only need the first element (or a specific part) of a delimited string. This provides an ~8-10x performance improvement and zero allocations.
