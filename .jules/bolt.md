## 2026-06-07 - Avoid strings.Split for Single Substring Extractions in Hot Paths
**Learning:** In HTTP middleware (`internal/auth/proxy.go`) and request handlers (`internal/handlers/sharing.go`), using `strings.Split` to extract the first element of a comma-separated list (e.g., from `X-Forwarded-For` or splitting an email) unnecessarily allocates a string slice on the heap for every request.
**Action:** Use `strings.IndexByte` to find the delimiter index and use string slicing (`str[:idx]`) instead. This provides a ~10x performance improvement with 0 allocations on hot paths.
