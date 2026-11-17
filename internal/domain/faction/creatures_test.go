package faction

import (
	"testing"

	"rimworld-go/internal/domain/animal"
)

func TestCreatureAllegiance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s      animal.Species
		want   Type
		hidden bool
	}{
		{animal.HiveBeetle, Swarm, true},
		{animal.HiveCrawler, Swarm, true},
		{animal.HiveBrute, Swarm, true},
		{animal.Harrow, MechHive, true},
		{animal.Spindle, MechHive, true},
		{animal.Bulwark, MechHive, true},
		{animal.Quill, MechHive, true},
		{animal.Deer, Player, false},
		{animal.TimberWolf, Player, false},
		{animal.Species(255), Player, false},
	} {
		got, ok := Creature(tc.s)
		if got != tc.want || ok != tc.hidden {
			t.Errorf("species %s: faction=%s hidden=%v", tc.s, got, ok)
		}
		if ok && !DefOf(got).Hidden {
			t.Fatalf("creature %s belongs to surface faction", tc.s)
		}
	}
}

func TestHiddenFactionHostility(t *testing.T) {
	t.Parallel()
	for _, a := range []Type{Player, Settlers, Clans, Freebooters, AncientHostile, Swarm, MechHive} {
		for _, b := range []Type{Player, Settlers, Clans, Freebooters, AncientHostile, Swarm, MechHive} {
			want := a != b && (a == Swarm || b == Swarm || a == MechHive || b == MechHive)
			if got := HiddenEnemies(a, b); got != want || got != HiddenEnemies(b, a) {
				t.Errorf("hidden hostility %v/%v = %v, want %v", a, b, got, want)
			}
		}
	}
}
