package digest

import "testing"

func TestXXH3EmptyVector(t *testing.T) {
	if got, want := XXH3(nil), uint64(0x2d06800538d394c2); got != want {
		t.Fatalf("XXH3(nil) = %016x, want %016x", got, want)
	}
}
