package game

import (
	"fmt"
	"testing"

	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/pawn/mood"
	"rimworld-go/internal/domain/pawn/social"
	"rimworld-go/internal/domain/sim/entity"
)

var benchMood int

func BenchmarkMoodLookup(b *testing.B) {
	for _, n := range []int{4, 16, 64} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			g := New(Config{Seed: 1, Width: 14, Height: 10})
			id, _ := g.SpawnNamedPawn(5, 5, "Ada")
			now := uint64(g.clock.Now())
			memories := make(mood.Memories, n)
			for i := range memories {
				memories[i] = mood.Memory{Kind: mood.KindWords, Expires: now + 1000, Subject: uint64(i + 1)}
			}
			g.memories[id] = memories
			g.stepMood(id)
			b.Run("smoothed", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					benchMood = g.moodOf(id)
				}
			})
			b.Run("target", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					benchMood = g.thoughtLevel(id)
				}
			})
		})
	}
}

func BenchmarkGriefRefresh(b *testing.B) {
	g := New(Config{Seed: 1, Width: 14, Height: 10})
	id, _ := g.SpawnNamedPawn(5, 5, "Ada")
	d := social.GriefDef{Mood: -14, Days: 30}
	now := uint64(g.clock.Now())
	g.addGrief(id, entity.EntityID(777), d, "my sister died", now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.addGrief(id, entity.EntityID(777), d, "my sister died", now)
	}
}

func BenchmarkDailyMemoryCleanup(b *testing.B) {
	for _, n := range []int{4, 64, 256} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			g := New(Config{Seed: 1, Width: 14, Height: 10})
			for i := 0; i < n; i++ {
				id := entity.EntityID(i + 100)
				g.memories[id] = mood.Memories{{Kind: mood.KindWords, Expires: 100000}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g.socialDay(90000)
			}
		})
	}
}

func BenchmarkReturnedGriefCleanup(b *testing.B) {
	g, a, subject := socialPair(b)
	g.setPair(a, subject, social.Pair{Kind: social.PairSpouse, Since: uint64(g.clock.Now())})
	mem := mood.Memories{
		{Kind: mood.Grief, Subject: uint64(subject), Expires: 40000, Label: "my spouse is missing"},
		{Kind: mood.KindWords, Subject: 1000, Expires: 40000},
	}
	work := make(mood.Memories, len(mem))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(work, mem)
		g.memories[a] = work
		g.socialOnReturned(subject)
		g.history = g.history[:0]
	}
}

var benchMoodTarget int

func BenchmarkMoodBeforeFirstStep(b *testing.B) {
	g := New(Config{Seed: 3, Width: 14, Height: 10})
	id, _ := g.SpawnNamedPawn(5, 5, "Ada")
	g.socState() // social data may be present before the first mood update
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchMood, benchMoodTarget = (doseHabits{g}).Mood(id)
	}
}

func BenchmarkMoodStepSleep(b *testing.B) {
	for _, sleep := range []bool{false, true} {
		for _, n := range []int{4, 16} {
			name := "awake"
			if sleep {
				name = "sleeping"
			}
			b.Run(fmt.Sprintf("%s/%d", name, n), func(b *testing.B) {
				g := New(Config{Seed: 3, Width: 14, Height: 10})
				id, _ := g.SpawnNamedPawn(5, 5, "Ada")
				mem := make(mood.Memories, n)
				for i := range mem {
					mem[i] = mood.Memory{Kind: mood.KindWords, Subject: uint64(i + 1), Expires: uint64(g.clock.Now()) + 10000}
				}
				g.memories[id] = mem
				g.stepMood(id)
				if sleep {
					g.jobs.Set(id, job.Record{Kind: job.Sleep, State: job.Do})
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					g.stepMood(id)
					benchMood = g.moodOf(id)
				}
			})
		}
	}
}
