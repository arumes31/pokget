## 2026-06-21 - Optimize X-Forwarded-For parsing
**Learning:** Using `strings.Split` for extracting the first item from a delimited string (like `X-Forwarded-For`) introduces unnecessary heap allocations for a slice of strings.
**Action:** Use `strings.IndexByte` combined with string slicing when only a specific part of the delimited string is needed, particularly in high-frequency paths like HTTP middlewares.
