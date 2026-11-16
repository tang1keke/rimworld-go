package difficulty

import "testing"

func BenchmarkDifficultyDecode(b *testing.B) {
	source := Encode(Of(Seasoned))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		value, err := Decode(source)
		if err != nil {
			b.Fatal(err)
		}
		benchSettings = value
	}
}
