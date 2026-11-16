package difficulty

import "testing"

var benchAdapt Adapter

func BenchmarkAdaptationDay(b *testing.B) {
	s := Of(Seasoned)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchAdapt.Day(s, Setback{})
	}
}
