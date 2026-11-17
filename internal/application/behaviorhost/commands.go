package behaviorhost

import (
	"fmt"
	"rimworld-go/internal/application/game"
	"rimworld-go/internal/application/game/behavior"
	"rimworld-go/internal/domain/building/construction"
	"rimworld-go/internal/domain/building/zone"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/economy/recipe"
	"rimworld-go/internal/domain/sim/command"
	"rimworld-go/internal/domain/world/terrain"
	"slices"
)

// submit converts the entire permission-checked proposal before enqueuing.
// Native phaseCommands applies all queued commands the next simulation tick.
func (h *Runner) submit(provider string, intents []behavior.Intent) error {
	g := h.g
	selected := g.ViewedMap()
	defer g.ViewMap(selected)
	var translated []command.Command
	seen := map[[3]int]bool{}
	for _, in := range intents {
		found := false
		for _, m := range g.Maps() {
			if uint32(m.ID) == in.Map {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: unknown map %d", provider, in.Map)
		}
		g.ViewMap(game.MapID(in.Map))
		c, err := commandFor(g, g.World(), in)
		if err != nil {
			return fmt.Errorf("%s %s: %w", provider, in.Action, err)
		}
		if in.Action == behavior.PlaceFrame || in.Action == behavior.SwapMaterial {
			key := [3]int{int(in.Map), c.X, c.Y}
			if seen[key] {
				return fmt.Errorf("%s: repeated placement at %d,%d", provider, c.X, c.Y)
			}
			seen[key] = true
		}
		c.Map, c.MapSet = in.Map, true
		translated = append(translated, c)
	}
	for _, c := range translated {
		g.Enqueue(c)
	}
	return nil
}

func commandFor(g *game.Game, v game.WorldView, in behavior.Intent) (command.Command, error) {
	switch in.Action {
	case behavior.PlaceFrame, behavior.SwapMaterial:
		var kind construction.Kind
		var facing construction.Facing
		if in.Action == behavior.SwapMaterial {
			b, ok := findBuilding(v, uint64(in.Target))
			if !ok || b.Ruin || b.Hostile || b.Deconstruct {
				return command.Command{}, fmt.Errorf("ineligible building %d", in.Target)
			}
			if in.Build != "" && in.Build != b.Kind {
				return command.Command{}, fmt.Errorf("kind mismatch")
			}
			if (in.X != 0 || in.Y != 0) && (in.X != b.X || in.Y != b.Y) {
				return command.Command{}, fmt.Errorf("position mismatch")
			}
			var exists bool
			kind, exists = construction.Parse(b.Kind)
			if !exists {
				return command.Command{}, fmt.Errorf("unsupported building kind %q", b.Kind)
			}
			facing, exists = parseFacing(b.Facing)
			if !exists {
				return command.Command{}, fmt.Errorf("bad existing building facing")
			}
			in.X, in.Y = b.X, b.Y
			if b.Stuff == in.Stuff {
				return command.Command{}, fmt.Errorf("already that material")
			}
		} else {
			var exists bool
			kind, exists = construction.Parse(in.Build)
			if !exists {
				return command.Command{}, fmt.Errorf("unknown build kind %q", in.Build)
			}
			facing, exists = parseFacing(in.Facing)
			if !exists {
				return command.Command{}, fmt.Errorf("invalid facing %q", in.Facing)
			}
			// A new frame may not disguise a material swap.
			for _, b := range v.Buildings {
				if b.X == in.X && b.Y == in.Y && b.Kind == in.Build {
					return command.Command{}, fmt.Errorf("existing building at placement")
				}
			}
		}
		stuff, ok := item.ParseKind(in.Stuff)
		if !ok || !construction.StuffOK(kind, stuff) || !kind.Valid() || kind.Planned() || construction.DefOf(kind).Unbuildable {
			return command.Command{}, fmt.Errorf("invalid building/material %q/%q", in.Build, in.Stuff)
		}
		if permitted, why := g.CanPlaceAt(kind.String(), in.Stuff, facing.String(), in.X, in.Y); !permitted {
			return command.Command{}, fmt.Errorf("placement rejected: %s", why)
		}
		return command.Command{Kind: command.DesignateBuild, Build: uint8(kind), Stuff: uint8(stuff), Facing: uint8(facing), X: in.X, Y: in.Y}, nil
	case behavior.DesignateMine:
		width := v.TerrainW
		if width <= 0 || in.X < 0 || in.Y < 0 || in.X >= width || in.Y >= g.Height() {
			return command.Command{}, fmt.Errorf("outside map")
		}
		ix := in.Y*width + in.X
		if ix >= len(v.Terrain) || !terrain.IsRockWall(terrain.Type(v.Terrain[ix])) || ix < len(v.Fog) && v.Fog[ix] != 0 {
			return command.Command{}, fmt.Errorf("not a visible mineable rock")
		}
		return command.Command{Kind: command.DesignateMine, X: in.X, Y: in.Y, X2: in.X, Y2: in.Y}, nil
	case behavior.DesignateDeconstruct:
		b, ok := findBuilding(v, in.Target)
		if !ok || b.Ruin || b.Hostile || b.Deconstruct {
			return command.Command{}, fmt.Errorf("invalid deconstruct target %d", in.Target)
		}
		return command.Command{Kind: command.DesignateDeconstruct, X: b.X, Y: b.Y, X2: b.X, Y2: b.Y}, nil
	case behavior.MarkHaul:
		if !slices.ContainsFunc(v.Items, func(it game.ItemView) bool { return uint64(it.ID) == in.Target }) {
			return command.Command{}, fmt.Errorf("missing stack %d", in.Target)
		}
		return command.Command{Kind: command.SetHaulMark, Target: in.Target, Enabled: in.Enabled}, nil
	case behavior.SetHaulPriority:
		if in.Priority < int(zone.PriorityLow) || in.Priority > int(zone.PriorityCritical) {
			return command.Command{}, fmt.Errorf("bad priority")
		}
		valid := false
		for _, z := range v.Zones {
			if uint64(z.ID) == in.Target && (z.Kind == "stockpile" || z.Kind == "dump" || z.Kind == "shelf" || z.Kind == "hopper" || z.Kind == "bookcase") {
				valid = true
				break
			}
		}
		if !valid {
			return command.Command{}, fmt.Errorf("invalid storage target %d", in.Target)
		}
		index := slices.Index(zone.Priorities[:], zone.Priority(in.Priority))
		if index < 0 {
			return command.Command{}, fmt.Errorf("invalid priority index")
		}
		return command.Command{Kind: command.SetZonePriority, Target: in.Target, Index: index}, nil
	case behavior.SetBill, behavior.AddBill:
		b, ok := findBuilding(v, in.Target)
		if !ok || b.Ruin || b.Hostile || b.Deconstruct {
			return command.Command{}, fmt.Errorf("invalid bench %d", in.Target)
		}
		kind, ok := construction.Parse(b.Kind)
		if !ok {
			return command.Command{}, fmt.Errorf("invalid bench kind")
		}
		mode, err := billMode(in.Mode)
		if err != nil {
			return command.Command{}, err
		}
		if in.Count < 0 || in.Count > 100000 || in.Index < 0 {
			return command.Command{}, fmt.Errorf("invalid bill size/index")
		}
		if in.Action == behavior.SetBill {
			if in.Index >= len(b.Bills) {
				return command.Command{}, fmt.Errorf("missing bill index %d", in.Index)
			}
			return command.Command{Kind: command.SetBill, X: b.X, Y: b.Y, Index: in.Index, Mode: uint8(mode), Count: in.Count, Enabled: in.Enabled}, nil
		}
		if _, locked := v.Locked["recipe:"+in.Recipe]; locked {
			return command.Command{}, fmt.Errorf("recipe locked")
		}
		for _, d := range recipe.All() {
			if d.Name == in.Recipe && d.At(kind) {
				return command.Command{Kind: command.AddBill, X: b.X, Y: b.Y, Recipe: uint8(d.ID), Mode: uint8(mode), Count: in.Count}, nil
			}
		}
		return command.Command{}, fmt.Errorf("unknown bench recipe %q", in.Recipe)
	default:
		return command.Command{}, fmt.Errorf("unsupported intent %q", in.Action)
	}
}

func findBuilding(v game.WorldView, id uint64) (game.BuildingView, bool) {
	for _, b := range v.Buildings {
		if uint64(b.ID) == id {
			return b, true
		}
	}
	return game.BuildingView{}, false
}

func parseFacing(name string) (construction.Facing, bool) {
	if name == "" {
		return construction.South, true
	}
	for _, v := range []construction.Facing{construction.North, construction.East, construction.South, construction.West} {
		if v.String() == name {
			return v, true
		}
	}
	return 0, false
}

func billMode(name string) (recipe.Mode, error) {
	switch name {
	case "", "count":
		return recipe.DoCount, nil
	case "forever":
		return recipe.Forever, nil
	case "until_have":
		return recipe.UntilHave, nil
	default:
		return 0, fmt.Errorf("invalid bill mode %q", name)
	}
}
