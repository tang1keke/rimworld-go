package recreation

import (
	"strconv"
	"testing"
)

var benchDecay int

func BenchmarkToleranceDecayByWealth(b *testing.B) {
	for _, w := range []int{0, 15000, 81000, 308000} {
		b.Run("wealth"+strconv.Itoa(w), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchDecay = DecayPct(w)
			}
		})
	}
}
