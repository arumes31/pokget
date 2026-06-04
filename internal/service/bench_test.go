package service

import (
	"testing"
)

func BenchmarkGetProgressToNextRank(b *testing.B) {
	s := NewGamificationService(nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.GetProgressToNextRank(100)
		s.GetProgressToNextRank(1500)
		s.GetProgressToNextRank(60000)
		s.GetProgressToNextRank(300000)
	}
}
