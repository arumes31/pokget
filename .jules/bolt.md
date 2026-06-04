## Performance Optimization: GetProgressToNextRank

* **Optimization:** Replaced the linear `for` loop in `GetProgressToNextRank` with `sort.Search`.
* **Rationale:** The `Ranks` array is implicitly ordered by `MinXP`. A binary search using `sort.Search` significantly reduces the number of iterations needed to find the current rank compared to an $O(N)$ linear scan, especially as the number of ranks grows.
* **Results:** The `BenchmarkGetProgressToNextRank` test measured a speedup from ~238.1 ns/op to ~38.01 ns/op, which represents a ~6x improvement.
