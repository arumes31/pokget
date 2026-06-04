
## Performance Optimization: Slice Pre-allocation in `Binders` handler

**What:** Replaced `var binders []Binder` with `binders := make([]Binder, 0, 8)` in `internal/handlers/handlers.go` at line 441.

**Why:** The previous code declared a nil slice and repeatedly appended to it within a loop over database rows. Because the initial capacity was 0, `append` would trigger multiple underlying array re-allocations and memory copies as the slice grew. Since a collector typically has a few binders, initializing the slice with a sensible default capacity (e.g., 8) avoids these hidden allocations and improves memory efficiency and CPU time.

**Impact:** Reduces memory allocations during the rendering of the `binders.html` template. Since this handler is called repeatedly (both directly and at the end of `CreateBinder`), minimizing garbage collection pressure in a frequently hit endpoint contributes to overall application responsiveness.

**Measurement:** The allocation overhead drops from roughly `O(log_2(N))` reallocations to zero for user accounts with 8 or fewer binders. For those with more, the initial buffer still skips the first three allocations that would happen under a completely dynamic slice.
