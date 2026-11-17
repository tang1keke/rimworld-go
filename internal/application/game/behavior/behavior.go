// Package behavior defines the stable, value-only boundary for trusted Go
// behavior providers. The simulation, not providers, owns commands and jobs.
// Untrusted code must use a future sandboxed Starlark/WASM adapter.
package behavior

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

const (
	APIMajor uint32 = 1
	APIMinor uint32 = 0
)

// Capability is an explicit permission granted to a provider at registration.
type Capability string

const (
	ReadWorld             Capability = "world.read"
	ProvideIntents        Capability = "intent.propose"
	PlaceFrames           Capability = "command.place_frame"
	SwapMaterials         Capability = "command.swap_material"
	DesignateMines        Capability = "command.mine"
	DesignateDeconstructs Capability = "command.deconstruct"
	ConfigureBills        Capability = "command.bill"
	ConfigureHaul         Capability = "command.haul"
)

// Action denotes a *native* command, not an independently simulated job.
type Action string

const (
	PlaceFrame           Action = "place_frame"
	SwapMaterial         Action = "swap_material"
	DesignateMine        Action = "designate_mine"
	DesignateDeconstruct Action = "designate_deconstruct"
	SetBill              Action = "set_bill"
	AddBill              Action = "add_bill"
	MarkHaul             Action = "mark_haul"
	SetHaulPriority      Action = "set_haul_priority"
)

// Intent contains stable identifiers, coordinates and names rather than game
// pointers. X/Y designate a cell; Target identifies a building/stack/zone.
// Build and Stuff are the native, stable definition names.
// Mode for bills is "count", "forever" or "until_have"; Priority is -1..3.
type Intent struct {
	Action   Action
	Map      uint32
	Target   uint64
	X, Y     int
	Build    string
	Stuff    string
	Facing   string
	Recipe   string
	Index    int
	Mode     string
	Count    int
	Enabled  bool
	Priority int
}

type Manifest struct {
	ID           string
	Major, Minor uint32
	Capabilities []Capability
	EveryTicks   uint64
	StateVersion uint32
}

// Provider is called synchronously in deterministic ID order on the game
// writer. Propose is given detached value snapshots and a copy of its state.
// Return nil State to retain previous bytes; non-nil bytes replace them only
// after *all* proposed intents pass validation and are queued.
type Provider interface {
	Manifest() Manifest
	Propose(World, []byte) (Proposal, error)
}

// Migrator is optional. A provider with an older saved StateVersion must
// implement it, or registration fails rather than silently dropping state.
type Migrator interface {
	Migrate(from uint32, data []byte) ([]byte, error)
}

type Proposal struct {
	Intents []Intent
	State   []byte
}

// StoredState is append-only save data; unknown providers survive load/save.
// Data is opaque to the core. Use canonical encoding inside each provider.
type StoredState struct {
	ID      string
	Version uint32
	Data    []byte
}

type Failure struct {
	ID    string
	Tick  uint64
	Cause string
}

type Pawn struct {
	ID                                       uint64
	Name                                     string
	X, Y                                     int
	Alive, Colonist                          bool
	Job                                      string
	CanConstruct, CanMine, CanHaul, CanCraft bool
}
type Building struct {
	ID                         uint64
	Build, Stuff, Facing       string
	X, Y                       int
	Deconstruct, Ruin, Hostile bool
}
type Frame struct {
	ID                        uint64
	Build, Stuff              string
	X, Y                      int
	Need, Delivered, WorkLeft int
}
type Item struct {
	ID                               uint64
	Kind                             string
	Count, X, Y                      int
	Forbidden, Unclaimed, HaulMarked bool
}
type Stockpile struct {
	ID       uint64
	Kind     string
	Priority int
	Cells    [][2]int
}
type Rock struct {
	X, Y       int
	Stone, Ore string
	MineMarked bool
}
type Room struct {
	ID, Cells int
	Role      string
	Roofed    float64
}
type Research struct {
	Name                      string
	Done, Available, Selected bool
}
type Map struct {
	ID            uint32
	Width, Height int
	Pawns         []Pawn
	Buildings     []Building
	Frames        []Frame
	Items         []Item
	Stockpiles    []Stockpile
	Rocks         []Rock
	Rooms         []Room
	Research      []Research
	// The first API slice has no pathing-region query: intentionally absent.
	ColonistRegions []int32
}

// ColonistRegionsAt reads a movement component without exposing writable
// storage; -1 indicates blocked or out of bounds.
func (m Map) ColonistRegionsAt(x, y int) int32 {
	if uint(x) >= uint(m.Width) || uint(y) >= uint(m.Height) || len(m.ColonistRegions) != m.Width*m.Height {
		return -1
	}
	return m.ColonistRegions[y*m.Width+x]
}

type World struct {
	Tick uint64
	Maps []Map
}

// Clone prevents a provider from modifying another provider's view.
func (v World) Clone() World {
	out := World{Tick: v.Tick, Maps: make([]Map, len(v.Maps))}
	for i, m := range v.Maps {
		m.Pawns = slices.Clone(m.Pawns)
		m.Buildings = slices.Clone(m.Buildings)
		m.Frames = slices.Clone(m.Frames)
		m.Items = slices.Clone(m.Items)
		m.Stockpiles = slices.Clone(m.Stockpiles)
		for j := range m.Stockpiles {
			m.Stockpiles[j].Cells = slices.Clone(m.Stockpiles[j].Cells)
		}
		m.Rocks = slices.Clone(m.Rocks)
		m.Rooms = slices.Clone(m.Rooms)
		m.Research = slices.Clone(m.Research)
		m.ColonistRegions = slices.Clone(m.ColonistRegions)
		out.Maps[i] = m
	}
	return out
}

type registered struct {
	provider Provider
	manifest Manifest
	caps     map[Capability]bool
}

type Registry struct {
	providers []registered
	states    map[string]StoredState
	failures  []Failure
}

func NewRegistry() *Registry { return &Registry{states: make(map[string]StoredState)} }

// CloneStates detaches both the entry slice and each provider-owned byte blob.
func CloneStates(in []StoredState) []StoredState {
	out := slices.Clone(in)
	for i := range out {
		out[i].Data = slices.Clone(in[i].Data)
	}
	return out
}

func validID(s string) bool {
	if len(s) < 3 || len(s) > 128 || !strings.Contains(s, ".") {
		return false
	}
	for _, segment := range strings.Split(s, ".") {
		if segment == "" {
			return false
		}
		for _, r := range segment {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func capabilityOf(a Action) Capability {
	switch a {
	case PlaceFrame:
		return PlaceFrames
	case SwapMaterial:
		return SwapMaterials
	case DesignateMine:
		return DesignateMines
	case DesignateDeconstruct:
		return DesignateDeconstructs
	case SetBill, AddBill:
		return ConfigureBills
	case MarkHaul, SetHaulPriority:
		return ConfigureHaul
	}
	return ""
}

func allowedCapability(c Capability) bool {
	switch c {
	case ReadWorld, ProvideIntents, PlaceFrames, SwapMaterials, DesignateMines, DesignateDeconstructs, ConfigureBills, ConfigureHaul:
		return true
	}
	return false
}

func (r *Registry) Register(p Provider) error {
	if p == nil || reflect.ValueOf(p).Kind() == reflect.Ptr && reflect.ValueOf(p).IsNil() {
		return fmt.Errorf("behavior: nil provider")
	}
	m := p.Manifest()
	if !validID(m.ID) || m.Major != APIMajor || m.Minor > APIMinor || m.EveryTicks == 0 || m.StateVersion == 0 {
		return fmt.Errorf("behavior: invalid manifest for %q", m.ID)
	}
	caps := make(map[Capability]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		if !allowedCapability(c) || caps[c] {
			return fmt.Errorf("behavior %s: unsupported or duplicated capability %q", m.ID, c)
		}
		caps[c] = true
	}
	if !caps[ReadWorld] || !caps[ProvideIntents] {
		return fmt.Errorf("behavior %s: read and intent capabilities are required", m.ID)
	}
	for _, e := range r.providers {
		if e.manifest.ID == m.ID {
			return fmt.Errorf("behavior: duplicate provider %s", m.ID)
		}
	}
	if r.states == nil {
		r.states = make(map[string]StoredState)
	}
	st, saved := r.states[m.ID]
	if saved {
		if st.Version > m.StateVersion {
			return fmt.Errorf("behavior %s: state version %d is newer than %d", m.ID, st.Version, m.StateVersion)
		}
		if st.Version < m.StateVersion {
			mig, ok := p.(Migrator)
			if !ok {
				return fmt.Errorf("behavior %s: missing migration %d -> %d", m.ID, st.Version, m.StateVersion)
			}
			data, err := mig.Migrate(st.Version, slices.Clone(st.Data))
			if err != nil {
				return fmt.Errorf("behavior %s: migrate: %w", m.ID, err)
			}
			st.Version, st.Data = m.StateVersion, slices.Clone(data)
		}
	} else {
		st = StoredState{ID: m.ID, Version: m.StateVersion}
	}
	r.states[m.ID] = st
	r.providers = append(r.providers, registered{provider: p, manifest: m, caps: caps})
	slices.SortFunc(r.providers, func(a, b registered) int { return strings.Compare(a.manifest.ID, b.manifest.ID) })
	return nil
}

func (r *Registry) Restore(values []StoredState) error {
	if len(r.providers) != 0 {
		return fmt.Errorf("behavior: restore before register")
	}
	states := make(map[string]StoredState, len(values))
	for _, s := range values {
		if !validID(s.ID) || s.Version == 0 {
			return fmt.Errorf("behavior: invalid saved ID or state version %q", s.ID)
		}
		if _, seen := states[s.ID]; seen {
			return fmt.Errorf("behavior: duplicated saved state %s", s.ID)
		}
		s.Data = slices.Clone(s.Data)
		states[s.ID] = s
	}
	r.states = states
	return nil
}

func (r *Registry) Export() []StoredState {
	var result []StoredState
	for _, s := range r.states {
		s.Data = slices.Clone(s.Data)
		result = append(result, s)
	}
	slices.SortFunc(result, func(a, b StoredState) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func (r *Registry) Missing() []string {
	var out []string
	for _, s := range r.Export() {
		found := false
		for _, p := range r.providers {
			if p.manifest.ID == s.ID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s.ID)
		}
	}
	return out
}

func (r *Registry) Failures() []Failure { return slices.Clone(r.failures) }

func propose(p Provider, v World, state []byte) (out Proposal, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("panic: %v", x)
		}
	}()
	return p.Propose(v, state)
}

// Step calls providers in ID order. Commands are committed as a complete
// proposal; no new job type is created and no simulation RNG is consumed.
func (r *Registry) Step(tick uint64, query func() World, submit func(string, []Intent) error) {
	var view World
	built := false
	for _, e := range r.providers {
		if tick%e.manifest.EveryTicks != 0 {
			continue
		}
		if !built {
			r.failures = nil // retain last due-tick diagnostics across idle ticks
			view, built = query(), true
		}
		old := r.states[e.manifest.ID]
		proposal, err := propose(e.provider, view.Clone(), slices.Clone(old.Data))
		if err == nil && len(proposal.Intents) > 64 {
			err = fmt.Errorf("too many intents: %d", len(proposal.Intents))
		}
		if err == nil && len(proposal.State) > 65536 {
			err = fmt.Errorf("provider state exceeds 64 KiB")
		}
		if err == nil {
			for _, intent := range proposal.Intents {
				cap := capabilityOf(intent.Action)
				if cap == "" || !e.caps[cap] {
					err = fmt.Errorf("capability denied: %s", intent.Action)
					break
				}
			}
		}
		if err == nil && len(proposal.Intents) > 0 {
			err = submit(e.manifest.ID, proposal.Intents)
		}
		if err != nil {
			r.failures = append(r.failures, Failure{ID: e.manifest.ID, Tick: tick, Cause: err.Error()})
			continue
		}
		if proposal.State != nil {
			old.Data = slices.Clone(proposal.State)
			r.states[e.manifest.ID] = old
		}
	}
}
