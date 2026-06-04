package service

import (
	"strings"
)

// levenshtein calculates the Levenshtein distance between two strings.
// Optimized to use O(min(N, M)) memory space and to decrease execution
// time. The original implementation used an O(N * M) matrix.
// This reduces heap allocations and garbage collector pressure.
func levenshtein(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)
	n, m := len(s1), len(s2)
	if n == 0 { return m }
	if m == 0 { return n }

	if n < m {
		n, m = m, n
		s1, s2 = s2, s1
	}

	v0 := make([]int, m+1)
	v1 := make([]int, m+1)

	for i := 0; i <= m; i++ {
		v0[i] = i
	}

	for i := 0; i < n; i++ {
		v1[0] = i + 1
		for j := 0; j < m; j++ {
			cost := 1
			if s1[i] == s2[j] {
				cost = 0
			}
			v1[j+1] = min(v1[j]+1, min(v0[j+1]+1, v0[j]+cost))
		}
		v0, v1 = v1, v0
	}

	return v0[m]
}
