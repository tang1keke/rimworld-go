// Package savelegacy decodes historical skill-bearing gob fields. All
// conversions are invoked once on loading pre-v18 saved games.
package savelegacy

import (
	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/difficulty"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/pawn/backstory"
	"rimworld-go/internal/domain/pawn/look"
	"rimworld-go/internal/domain/pawn/pawngen"
	"rimworld-go/internal/domain/pawn/skill"
	"rimworld-go/internal/domain/pawn/trait"
	"rimworld-go/internal/domain/scenario"
	"rimworld-go/internal/domain/sim/entity"
	"rimworld-go/internal/domain/storyteller"
	"rimworld-go/internal/domain/world/planet"
	"rimworld-go/internal/domain/world/site"
	"rimworld-go/internal/domain/world/temperature"
)

// SkillSet is the wire width used by v1-v17 saved games.
type SkillSet [11]skill.Skill

type Pawn struct {
	First, Nick, Last    string
	Look                 look.Look
	AgeDays              int32
	Childhood, Adulthood backstory.ID
	Traits               []trait.ID
	Skills               SkillSet
	Conditions           []pawngen.Condition
	Temperament          pawngen.Temperament
}

type Learn struct {
	Day   int32
	Today [11]int32
	Week  [7][11]int32
	Carry [11]int16
}

type NeedCarry struct{ Hunger, Rest, Joy int32 }

type Working struct {
	Skill skill.ID
	Tick  uint64
}

type SkillLive struct {
	Learned   map[entity.EntityID]Learn
	NeedCarry map[entity.EntityID]NeedCarry
	Working   map[entity.EntityID]Working
	Research  int
}

type Setup struct {
	Scenario       scenario.Def
	Storyteller    string
	Preset         difficulty.Preset
	Difficulty     difficulty.Settings
	SaveMode       difficulty.SaveMode
	World          planet.Params
	Tile           int
	Site           site.Site
	Factions       []faction.Faction
	MapSize        int
	Season         uint8
	Colonists      []Pawn
	Seed           uint64
	GenVersion     uint16
	GoneAt         uint64
	WandererGroups int
}

type Extreme struct {
	Kind        uint8
	Delta       int
	Until, Cool uint64
}

type Pod struct {
	X, Y    int
	Tick    uint64
	Landed  bool
	Pawns   []int
	Items   []scenario.Stack
	Animals []animal.Species
}

type SetupState struct {
	Setup     Setup
	Factions  []faction.Faction
	Story     storyteller.State
	Plan      storyteller.Decision
	Adapt     difficulty.Adapter
	Climate   temperature.Climate
	StartDay  int
	Dead      []entity.EntityID
	Lost      bool
	Extreme   Extreme
	Pods      []Pod
	IntroDone bool
}

type Person struct {
	ID       entity.EntityID
	Bio      *Pawn
	Faction  int
	Where    uint8
	DiedAt   uint64
	KilledBy entity.EntityID
	Parents  [2]entity.EntityID
}

type People struct {
	Version uint8
	Seeded  bool
	Persons []Person
}
