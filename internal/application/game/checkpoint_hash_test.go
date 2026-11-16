package game

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"testing"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/domain/sim/statehash"
)

func TestCheckpointHashMatchesWrittenSave(t *testing.T) {
	check := func(t *testing.T, g *Game, stepped bool) {
		t.Helper()
		before := g.mapState
		expected := g.CheckpointHash()
		for _, slot := range []string{"colony", "autosave-2"} {
			var buf bytes.Buffer
			recorded, err := g.SaveCheckpoint(&buf, slot)
			if err != nil {
				t.Fatal(err)
			}
			if recorded != expected {
				t.Fatalf("slot %s: hash %x != checkpoint %x", slot, recorded, expected)
			}
			zr, err := savefmt.NewReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			dec := gob.NewDecoder(zr)
			var info SaveInfo
			var saved saveState
			if err := dec.Decode(&info); err != nil {
				t.Fatal(err)
			}
			if err := dec.Decode(&saved); err != nil {
				t.Fatal(err)
			}
			if err := zr.Close(); err != nil {
				t.Fatal(err)
			}
			info.Name = "statehash"
			decoded := statehash.OfFieldsXXH3(normalisedFieldsWith(info, &saved, statehash.OfFieldsXXH3))
			if recorded != decoded {
				t.Fatalf("slot %s: saved hash %x != decoded hash %x", slot, recorded, decoded)
			}
			loaded, _, err := Load(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			// NewColony has no roof/room initialization until its first tick.
			// The original FNV StateHash also changes across a tick-zero load.
			if stepped {
				if got := loaded.CheckpointHash(); got != recorded {
					t.Fatalf("slot %s: loaded hash %x != stored %x", slot, got, recorded)
				}
				if a, b := loaded.StateHash(), g.StateHash(); a != b {
					t.Fatalf("slot %s: FNV state hash after reload %x != %x", slot, a, b)
				}
			}
		}
		if g.mapState != before {
			t.Fatal("checkpoint changed the current map")
		}
	}
	for _, seed := range []uint64{1, 42} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			g := NewColony(Config{Seed: seed, Width: 48, Height: 40})
			t.Run("initial", func(t *testing.T) { check(t, g, false) })
			for _, steps := range []int{250, 3000, 20000} {
				g.Step(steps)
				t.Run(fmt.Sprintf("tick-%d", g.Tick()), func(t *testing.T) { check(t, g, true) })
			}
		})
	}
	t.Run("multi-map", func(t *testing.T) {
		g, _, _, _, _, _ := twoMaps(t)
		check(t, g, true)
		g.ViewMap(1)
		g.Step(10)
		check(t, g, true)
	})
}

func BenchmarkCheckpointHash(b *testing.B) {
	for _, side := range []int{64, 160} {
		b.Run(fmt.Sprintf("%d", side), func(b *testing.B) {
			g := NewColony(Config{Seed: 42, Width: side, Height: side})
			g.Step(180)
			b.Run("fnv", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = g.StateHash()
				}
			})
			b.Run("xxh3", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = g.CheckpointHash()
				}
			})
		})
	}
}
