package service

import (
	"strings"
)

// levenshtein calculates the Levenshtein distance between two strings.
// Optimized to use O(min(N, M)) space instead of O(N*M) by only keeping
// track of the previous and current rows during calculation.
func levenshtein(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)
	n, m := len(s1), len(s2)
	if n == 0 { return m }
	if m == 0 { return n }

	// Ensure n <= m to minimize memory usage for the rows
	if n > m {
		s1, s2 = s2, s1
		n, m = m, n
	}

	v0 := make([]int, n+1) // Previous row
	v1 := make([]int, n+1) // Current row

	for i := 0; i <= n; i++ {
		v0[i] = i
	}

	for i := 1; i <= m; i++ {
		v1[0] = i
		for j := 1; j <= n; j++ {
			cost := 1
			if s2[i-1] == s1[j-1] {
				cost = 0
			}
			v1[j] = min(v1[j-1]+1, min(v0[j]+1, v0[j-1]+cost))
		}
		v0, v1 = v1, v0 // Swap references instead of copying values
	}
	// Because of the final swap, the result is now in v0
	return v0[n]
}
