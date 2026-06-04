package service

import (
	"strings"
)

// levenshtein calculates the Levenshtein distance between two strings.
// Optimized to use O(min(N, M)) space instead of O(N*M) by only keeping
// the current and previous rows, reducing memory allocations significantly.
func levenshtein(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)
	n, m := len(s1), len(s2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	// Ensure m is the smaller dimension for memory efficiency
	if n < m {
		n, m = m, n
		s1, s2 = s2, s1
	}

	prev := make([]int, m+1)
	curr := make([]int, m+1)

	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		// Swap the rows to reuse the slice allocations
		prev, curr = curr, prev
	}

	// Result is in prev because we swap at the end of the loop
	return prev[m]
}
