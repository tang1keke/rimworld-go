package setupview

import (
	"reflect"
	"testing"

	"rimworld-go/internal/domain/faction"
)

func TestCloneFactionsPreservesIndependentWarRelations(t *testing.T) {
	in := []faction.Faction{{ID: 0, Type: faction.Drifters,
		Enemies: []int{1, 2}, Settlements: []int{44},
		History: []int16{-80}, Events: []string{"trade"}, TradeRemainder: 357,
		DriftDay: 4}}
	got := CloneFactions(in)
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("clone lost saved faction state: %+v", got)
	}
	got[0].Enemies[0] = 9
	got[0].Settlements[0] = 77
	got[0].History[0] = -20
	got[0].Events[0] = "raid"
	if in[0].Enemies[0] != 1 || in[0].Settlements[0] != 44 ||
		in[0].History[0] != -80 || in[0].Events[0] != "trade" {
		t.Fatal("cloned faction shares saved slice state with its source")
	}
}
