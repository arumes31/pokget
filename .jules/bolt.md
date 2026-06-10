## 2024-06-10 - String Split vs IndexByte
**Learning:** In highly trafficked middleware like `ProxyMiddleware`, using `strings.Split(str, ",")` to get just the first element allocates an entire slice on the heap. Using `strings.IndexByte(str, ',')` and slicing the string directly is a significant allocation win for zero overhead.
**Action:** Always prefer `strings.IndexByte` and string slicing over `strings.Split` when only extracting a prefix or suffix based on a delimiter, especially in middleware or loops.
