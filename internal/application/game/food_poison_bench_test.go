package game

import (
	"rimworld-go/internal/domain/economy/item"
	"testing"
)

func BenchmarkFoodPoisonRoll(b *testing.B) {
	g := New(Config{Seed: 6, Width: 12, Height: 8})
	id, _ := g.SpawnNamedPawn(2, 2, "Ada")
	st := item.Stack{Kind: item.Meal, Qty: 1, Poison: 20}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.rollFoodPoison(id, st)
	}
}
