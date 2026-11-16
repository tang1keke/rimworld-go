package behaviorhost

import (
	"bytes"
	"reflect"
	"rimworld-go/internal/application/game"
	"rimworld-go/internal/application/game/behavior"
	"rimworld-go/internal/domain/economy/item"
	"testing"
)

type testProvider struct {
	id   string
	deny bool
}

func (p testProvider) Manifest() behavior.Manifest {
	return behavior.Manifest{ID: p.id, Major: behavior.APIMajor, Minor: behavior.APIMinor,
		EveryTicks: 500, StateVersion: 1, Capabilities: []behavior.Capability{behavior.ReadWorld, behavior.ProvideIntents, behavior.PlaceFrames}}
}
func (p testProvider) Propose(w behavior.World, state []byte) (behavior.Proposal, error) {
	if len(state) > 0 {
		return behavior.Proposal{}, nil
	}
	cmds := []behavior.Intent{{Action: behavior.PlaceFrame, Build: "wall", Stuff: "wood", X: 5, Y: 4, Map: 0}}
	if p.deny {
		cmds = append(cmds, behavior.Intent{Action: behavior.MarkHaul, Map: 0, Target: 123})
	}
	return behavior.Proposal{Intents: cmds, State: []byte("submitted")}, nil
}

type scriptedProvider struct {
	manifest behavior.Manifest
	propose  func(behavior.World, []byte) (behavior.Proposal, error)
}

func (p scriptedProvider) Manifest() behavior.Manifest { return p.manifest }
func (p scriptedProvider) Propose(w behavior.World, data []byte) (behavior.Proposal, error) {
	return p.propose(w, data)
}

func TestBehaviorMultiMapSnapshotAndNativeTarget(t *testing.T) {
	g := game.New(game.Config{Seed: 3, Width: 24, Height: 16})
	id, ok := game.DevOpenMap(g, 5)
	if !ok {
		t.Fatal("could not open site map")
	}
	g.ViewMap(id)
	startTick := uint64(g.Tick())
	var seen int
	p := scriptedProvider{manifest: behavior.Manifest{ID: "test.multimap", Major: 1, EveryTicks: 500,
		StateVersion: 1, Capabilities: []behavior.Capability{behavior.ReadWorld, behavior.ProvideIntents, behavior.PlaceFrames}},
		propose: func(w behavior.World, _ []byte) (behavior.Proposal, error) {
			seen++
			if w.Tick != startTick || len(w.Maps) != 2 {
				t.Fatalf("wrong multi-map tick/count: tick=%d maps=%d", w.Tick, len(w.Maps))
			}
			if w.Maps[0].ID != 0 || w.Maps[1].ID != uint32(id) || w.Maps[1].Width != 60 {
				t.Fatalf("wrong multi-map identities: home=%d site=%d size=%d", w.Maps[0].ID, w.Maps[1].ID, w.Maps[1].Width)
			}
			return behavior.Proposal{Intents: []behavior.Intent{{Action: behavior.PlaceFrame,
				Map: uint32(id), Build: "wall", Stuff: "wood", X: 5, Y: 4}}}, nil
		}}
	h, err := Attach(g, p)
	if err != nil {
		t.Fatal(err)
	}
	g.Step(2)
	if seen != 1 || len(h.Failures()) != 0 || g.ViewedMap() != id {
		t.Fatalf("behavior or selected map changed: seen=%d failures=%v viewed=%d", seen, h.Failures(), g.ViewedMap())
	}
	if frames := g.World().Frames; len(frames) != 1 || frames[0].Kind != "wall" {
		t.Fatalf("site map frame missing: %+v", frames)
	}
	g.ViewMap(0)
	if frames := g.World().Frames; len(frames) != 0 {
		t.Fatalf("native command affected home map: %+v", frames)
	}
}

func TestBehaviorReattachKeepsLiveState(t *testing.T) {
	g := game.New(game.Config{Seed: 1, Width: 16, Height: 10})
	p := testProvider{id: "test.live"}
	first, err := Attach(g, p)
	if err != nil {
		t.Fatal(err)
	}
	g.Step(1)
	if got := first.Export(); len(got) != 1 || string(got[0].Data) != "submitted" {
		t.Fatalf("initial provider did not advance: %+v", got)
	}
	missing, err := Attach(g)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing.Missing(), []string{"test.live"}) {
		t.Fatalf("detached state missing: %v", missing.Missing())
	}
	restored, err := Attach(g, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.Export(); len(got) != 1 || string(got[0].Data) != "submitted" {
		t.Fatalf("reattach reverted live provider state: %+v", got)
	}
}

func TestBehaviorNativeFrameSaveReplay(t *testing.T) {
	p := testProvider{id: "test.native_frame"}
	g := game.New(game.Config{Seed: 1, Width: 16, Height: 10})
	h, err := Attach(g, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.SpawnNamedPawn(2, 2, "Builder"); err != nil {
		t.Fatal(err)
	}
	if _, err = g.SpawnStack(item.Wood, 100, 3, 3); err != nil {
		t.Fatal(err)
	}
	g.Step(2)
	world := g.World()
	if len(world.Frames) != 1 {
		t.Fatalf("native command did not create frame: frames=%v failures=%v", world.Frames, h.Failures())
	}
	if world.Frames[0].Kind != "wall" || world.Frames[0].Stuff != "wood" || world.Frames[0].Wood != 0 {
		t.Fatalf("invalid native frame: %+v", world.Frames[0])
	}
	if got := h.Export(); len(got) != 1 || string(got[0].Data) != "submitted" {
		t.Fatalf("provider state not committed: %+v", got)
	}
	var buf bytes.Buffer
	if err = g.Save(&buf, "behavior"); err != nil {
		t.Fatal(err)
	}
	restored, r, _, err := LoadWithProviders(bytes.NewReader(buf.Bytes()), p)
	if err != nil {
		t.Fatal(err)
	}
	if g.StateHash() != restored.StateHash() {
		t.Fatalf("save/load mismatch: %x != %x", g.StateHash(), restored.StateHash())
	}
	if !reflect.DeepEqual(h.Export(), r.Export()) {
		t.Fatalf("provider state changed: %+v %+v", h.Export(), r.Export())
	}
	for i := 0; i < 20; i++ {
		g.Step(50)
		restored.Step(50)
		if g.StateHash() != restored.StateHash() {
			t.Fatalf("replay diverged after %d ticks", (i+1)*50)
		}
	}
}

func TestBehaviorAtomicDeniedAndMissingSurviveResave(t *testing.T) {
	g := game.New(game.Config{Seed: 1, Width: 16, Height: 10})
	h, err := Attach(g, testProvider{id: "test.denied", deny: true})
	if err != nil {
		t.Fatal(err)
	}
	g.Step(2)
	if len(h.Failures()) != 1 || h.Failures()[0].ID != "test.denied" {
		t.Fatalf("no denied proposal diagnostic: %+v", h.Failures())
	}
	if len(g.World().Frames) != 0 || len(h.Export()[0].Data) != 0 {
		t.Fatal("partial command or state committed")
	}
	p := testProvider{id: "test.saved"}
	fresh := game.New(game.Config{Seed: 1, Width: 16, Height: 10})
	_, err = Attach(fresh, p)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Step(1)
	var first bytes.Buffer
	if err = fresh.Save(&first, "first"); err != nil {
		t.Fatal(err)
	}
	absent, _, err := game.Load(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := game.BehaviorStates(absent); len(got) != 1 || got[0].ID != "test.saved" {
		t.Fatalf("unknown state lost on plain Load: %+v", got)
	}
	var second bytes.Buffer
	if err = absent.Save(&second, "second"); err != nil {
		t.Fatal(err)
	}
	recovered, r, _, err := LoadWithProviders(bytes.NewReader(second.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Missing(), []string{"test.saved"}) {
		t.Fatalf("missing diagnostics %v", r.Missing())
	}
	if !reflect.DeepEqual(game.BehaviorStates(recovered), game.BehaviorStates(absent)) {
		t.Fatal("provider save state was dropped")
	}
	r2, err := Attach(recovered, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Missing()) != 0 {
		t.Fatalf("provider could not be restored: %v", r2.Missing())
	}
}
