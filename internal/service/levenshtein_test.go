package service

import "testing"

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		name string
		s1   string
		s2   string
		want int
	}{
		{
			name: "identical strings",
			s1:   "kitten",
			s2:   "kitten",
			want: 0,
		},
		{
			name: "one empty string",
			s1:   "kitten",
			s2:   "",
			want: 6,
		},
		{
			name: "both empty strings",
			s1:   "",
			s2:   "",
			want: 0,
		},
		{
			name: "one character substitution",
			s1:   "kitten",
			s2:   "sitten",
			want: 1,
		},
		{
			name: "case insensitivity",
			s1:   "Kitten",
			s2:   "kitten",
			want: 0,
		},
		{
			name: "case insensitivity with substitution",
			s1:   "Kitten",
			s2:   "sitten",
			want: 1,
		},
		{
			name: "insertions",
			s1:   "sitten",
			s2:   "sitting",
			want: 2,
		},
		{
			name: "deletions",
			s1:   "flaw",
			s2:   "lawn",
			want: 2,
		},
		{
			name: "completely different",
			s1:   "cat",
			s2:   "dog",
			want: 3,
		},
		{
			name: "longer strings",
			s1:   "execution",
			s2:   "intention",
			want: 5,
		},
		{
			name: "s2 longer than s1",
			s1:   "short",
			s2:   "longerstring",
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := levenshtein(tt.s1, tt.s2); got != tt.want {
				t.Errorf("levenshtein() = %v, want %v", got, tt.want)
			}
		})
	}
}
