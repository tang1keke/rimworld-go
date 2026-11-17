package behavior

import (
	"errors"
	"reflect"
	"testing"
)

type registryTestProvider struct {
	manifest Manifest
	call     func(World, []byte) (Proposal, error)
}

func (p registryTestProvider) Manifest() Manifest { return p.manifest }
func (p registryTestProvider) Propose(w World, b []byte) (Proposal, error) {
	if p.call == nil {
		return Proposal{}, nil
	}
	return p.call(w, b)
}

type registryMigrator struct{ registryTestProvider }

func (p registryMigrator) Migrate(from uint32, data []byte) ([]byte, error) {
	if from != 1 || len(data) != 1 {
		return nil, errors.New("bad migration input")
	}
	return []byte{data[0] + 1}, nil
}

func manifestFor(id string) Manifest {
	return Manifest{ID: id, Major: APIMajor, Minor: APIMinor, StateVersion: 1, EveryTicks: 5,
		Capabilities: []Capability{ReadWorld, ProvideIntents, PlaceFrames}}
}

func TestBehaviorRegistryRejectsInvalidNamespacedIDs(t *testing.T) {
	for _, id := range []string{"..", ".provider", "vendor.", "vendor..provider", "vendor/provider", "Vendor.provider"} {
		t.Run(id, func(t *testing.T) {
			if err := NewRegistry().Register(registryTestProvider{manifest: manifestFor(id)}); err == nil {
				t.Fatal("malformed manifest namespace accepted")
			}
			if err := NewRegistry().Restore([]StoredState{{ID: id, Version: 1}}); err == nil {
				t.Fatal("malformed saved namespace accepted")
			}
		})
	}
}

func TestBehaviorRegistryDeterministicOrderAndWorldIsolation(t *testing.T) {
	r := NewRegistry()
	var order []string
	mutating := registryTestProvider{
		manifest: manifestFor("test.alpha"),
		call: func(w World, state []byte) (Proposal, error) {
			if w.Maps[0].ColonistRegionsAt(0, 0) != 17 {
				t.Error("source region mismatch")
			}
			w.Maps[0].ColonistRegions[0] = 999
			w.Maps[0].Stockpiles[0].Cells[0] = [2]int{999, 999}
			return Proposal{Intents: []Intent{{Action: PlaceFrame}}, State: []byte{5}}, nil
		},
	}
	observer := registryTestProvider{
		manifest: manifestFor("test.beta"),
		call: func(w World, state []byte) (Proposal, error) {
			if w.Maps[0].ColonistRegionsAt(0, 0) != 17 || w.Maps[0].Stockpiles[0].Cells[0] != [2]int{1, 1} {
				t.Error("provider mutated another provider's snapshot")
			}
			return Proposal{Intents: []Intent{{Action: PlaceFrame}}, State: []byte{9}}, nil
		},
	}
	if err := r.Register(observer); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(mutating); err != nil {
		t.Fatal(err)
	}
	v := World{Tick: 20000, Maps: []Map{{Width: 2, Height: 2,
		ColonistRegions: []int32{17, 17, -1, -1},
		Stockpiles:      []Stockpile{{Cells: [][2]int{{1, 1}}}},
	}}}
	r.Step(20000, func() World { return v }, func(id string, cmds []Intent) error {
		order = append(order, id)
		if len(cmds) != 1 {
			t.Errorf("invalid command count %d", len(cmds))
		}
		return nil
	})
	if !reflect.DeepEqual(order, []string{"test.alpha", "test.beta"}) {
		t.Fatalf("order=%v", order)
	}
	if v.Maps[0].ColonistRegionsAt(0, 0) != 17 || v.Maps[0].Stockpiles[0].Cells[0] != [2]int{1, 1} {
		t.Fatal("registry mutated the authoritative view")
	}
	s := r.Export()
	if s[0].ID != "test.alpha" || s[1].ID != "test.beta" || s[0].Data[0] != 5 || s[1].Data[0] != 9 {
		t.Fatalf("export=%+v", s)
	}
	s[0].Data[0] = 77
	if r.Export()[0].Data[0] != 5 {
		t.Fatal("export leaks state storage")
	}
}

func TestBehaviorRegistryVersionMigrationAndMissingProvider(t *testing.T) {
	r := NewRegistry()
	original := []StoredState{{ID: "test.migration", Version: 1, Data: []byte{3}}, {ID: "test.absent", Version: 5, Data: []byte{7}}}
	if err := r.Restore(original); err != nil {
		t.Fatal(err)
	}
	m := manifestFor("test.migration")
	m.StateVersion = 2
	if err := r.Register(registryTestProvider{manifest: m}); err == nil {
		t.Fatal("older state accepted without migration hook")
	}
	if err := r.Register(registryMigrator{registryTestProvider{manifest: m}}); err != nil {
		t.Fatal(err)
	}
	if miss := r.Missing(); !reflect.DeepEqual(miss, []string{"test.absent"}) {
		t.Fatalf("missing=%v", miss)
	}
	states := r.Export()
	if states[0].ID != "test.absent" || states[0].Version != 5 || states[0].Data[0] != 7 {
		t.Fatalf("unknown state lost: %+v", states)
	}
	if states[1].ID != "test.migration" || states[1].Version != 2 || states[1].Data[0] != 4 {
		t.Fatalf("failed migration: %+v", states)
	}
	original[0].Data[0] = 88
	if r.Export()[1].Data[0] != 4 {
		t.Fatal("registry aliases restored storage")
	}
	future := NewRegistry()
	if err := future.Restore([]StoredState{{ID: "test.migration", Version: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := future.Register(registryMigrator{registryTestProvider{manifest: m}}); err == nil {
		t.Fatal("newer unknown state version accepted")
	}
}

func TestBehaviorRegistryPanicAndPermissionIsolation(t *testing.T) {
	r := NewRegistry()
	a := registryTestProvider{manifest: manifestFor("test.panics"), call: func(World, []byte) (Proposal, error) {
		panic("controlled panic")
	}}
	b := registryTestProvider{manifest: manifestFor("test.valid"), call: func(World, []byte) (Proposal, error) {
		return Proposal{State: []byte("ok")}, nil
	}}
	if err := r.Register(b); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(a); err != nil {
		t.Fatal(err)
	}
	r.Step(5, func() World { return World{} }, func(string, []Intent) error { return nil })
	failures := r.Failures()
	if len(failures) != 1 || failures[0].ID != "test.panics" {
		t.Fatalf("failures=%+v", failures)
	}
	if st := r.Export(); len(st) != 2 || len(st[0].Data) != 0 || string(st[1].Data) != "ok" {
		t.Fatalf("states=%+v", st)
	}
	r.Step(6, func() World { t.Fatal("must not query on idle tick"); return World{} }, func(string, []Intent) error { return nil })
	if len(r.Failures()) != 1 {
		t.Fatal("lost diagnostic on idle tick")
	}
}
