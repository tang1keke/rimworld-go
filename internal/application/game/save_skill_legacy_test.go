package game

import (
	"bytes"
	"encoding/gob"
	"os"
	"reflect"
	"testing"

	"rimworld-go/internal/application/game/savefmt"
	"rimworld-go/internal/application/game/savelegacy"
	"rimworld-go/internal/domain/pawn/skill"
	"rimworld-go/internal/domain/pawn/trait"
	"rimworld-go/internal/domain/sim/entity"
)

func TestV17SkillArraysMigrateIntoCanonicalFields(t *testing.T) {
	const id entity.EntityID = 71
	var skills savelegacy.SkillSet
	var learned savelegacy.Learn
	learned.Day = 16
	for i := range skills {
		skills[i] = skill.Skill{
			Level: uint8(i + 1), XP: int32(i * 71), Carry: int16(i * 3),
		}
		learned.Today[i] = int32(i + 91)
		learned.Carry[i] = int16(i + 4)
		for day := range learned.Week {
			learned.Week[day][i] = int32(i + day*100)
		}
	}
	skills[skill.Melee].Passion = skill.Major
	oldPawn := savelegacy.Pawn{First: "Ada", Nick: "Ada", Last: "Test",
		AgeDays: 1800, Skills: skills, Traits: []trait.ID{trait.Sunny}}

	// This wire payload has only v17 field names and array widths. It is
	// deliberately encoded through gob, as real historical saves are.
	wire := struct {
		Bios      map[entity.EntityID]savelegacy.Pawn
		Skills    map[entity.EntityID]savelegacy.SkillSet
		SkillLive *savelegacy.SkillLive
		NG1       *savelegacy.SetupState
		People    *savelegacy.People
	}{
		Bios:   map[entity.EntityID]savelegacy.Pawn{id: oldPawn},
		Skills: map[entity.EntityID]savelegacy.SkillSet{id: skills},
		SkillLive: &savelegacy.SkillLive{
			Learned:   map[entity.EntityID]savelegacy.Learn{id: learned},
			NeedCarry: map[entity.EntityID]savelegacy.NeedCarry{id: {Hunger: 8, Rest: 4, Joy: 1}},
			Working:   map[entity.EntityID]savelegacy.Working{id: {Skill: skill.Melee, Tick: 320}},
			Research:  37,
		},
		NG1: &savelegacy.SetupState{
			Adapt: savelegacy.Adapter{Days: 16, Value: 9},
			Setup: savelegacy.Setup{
				Seed: 123, Colonists: []savelegacy.Pawn{oldPawn},
				Difficulty: savelegacy.DifficultySettings{
					ThreatScale: 175, AdaptStrength: 60, ExtremeWeather: true,
					QuestRewardPct: 130, QuestThreats: true,
				},
			},
		},
		People: &savelegacy.People{Version: 1, Seeded: true, Persons: []savelegacy.Person{
			{ID: id + 1, Bio: &oldPawn, Faction: 2},
		}},
	}
	var raw bytes.Buffer
	if err := gob.NewEncoder(&raw).Encode(wire); err != nil {
		t.Fatal(err)
	}
	var s saveState
	if err := gob.NewDecoder(&raw).Decode(&s); err != nil {
		t.Fatalf("decode v17 wire layout: %v", err)
	}
	if err := s.migrateSkillArrays(); err != nil {
		t.Fatal(err)
	}

	want := skill.Set{}
	copy(want[:], skills[:])
	want[skill.Artistic].Level = skill.Neutral
	if got := s.Skills18[id]; got != want {
		t.Fatalf("migrated live skills %+v, want %+v", got, want)
	}
	pawn, ok := s.Bios18[id]
	if !ok || pawn.First != oldPawn.First || pawn.Last != oldPawn.Last ||
		!reflect.DeepEqual(pawn.Traits, oldPawn.Traits) || pawn.Skills != want {
		t.Fatalf("migrated identity %+v", pawn)
	}
	if s.SkillLive18 == nil || s.SkillLive18.Research != 37 {
		t.Fatalf("research/skill live %#v", s.SkillLive18)
	}
	log := s.SkillLive18.Learned[id]
	if log.Day != learned.Day || log.Today[skill.Artistic] != 0 ||
		log.Carry[skill.Artistic] != 0 {
		t.Fatalf("learn log header %+v", log)
	}
	for i := range skills {
		if log.Today[i] != learned.Today[i] || log.Carry[i] != learned.Carry[i] {
			t.Fatalf("learn log skill %d changed", i)
		}
		for day := range log.Week {
			if log.Week[day][i] != learned.Week[day][i] {
				t.Fatalf("learn log week %d skill %d changed", day, i)
			}
		}
	}
	if s.SkillLive18.NeedCarry[id].Hunger != 8 ||
		s.SkillLive18.Working[id].Tick != 320 {
		t.Fatal("fractional work and need carries were dropped")
	}
	if s.NG118 == nil || s.NG118.Setup.Seed != 123 ||
		len(s.NG118.Setup.Colonists) != 1 || s.NG118.Setup.Colonists[0].Skills != want {
		t.Fatal("wizard colonist skills were not migrated")
	}
	if got := s.NG118.Adapt; got.Days != 16 || got.Value != 9 || got.GrowthCarry != 0 {
		t.Fatalf("legacy adaptation state %+v", got)
	}
	settings := s.NG118.Setup.Difficulty
	if settings.ThreatScale != 175 || settings.AdaptStrength != 60 ||
		!settings.ExtremeWeather || settings.QuestRewardPct != 130 || !settings.QuestThreats {
		t.Fatalf("legacy difficulty settings %+v", settings)
	}
	if settings.AdaptGrowthPercent() != 100 || settings.FoodPoisonPercent() != 100 {
		t.Fatal("new difficulty multipliers must default to neutral for legacy saves")
	}
	if s.People18 == nil || len(s.People18.Persons) != 1 ||
		s.People18.Persons[0].Bio == nil || s.People18.Persons[0].Bio.Skills != want {
		t.Fatal("off-map person skills were not migrated")
	}
	if s.Bios != nil || s.Skills != nil || s.SkillLive != nil || s.NG1 != nil || s.People != nil {
		t.Fatal("v17 wire fields remained populated after migration")
	}

	// A v18 body is self-contained: historical fields are absent when
	// loaded again, and no second migration is necessary.
	raw.Reset()
	if err := gob.NewEncoder(&raw).Encode(s); err != nil {
		t.Fatal(err)
	}
	var current saveState
	if err := gob.NewDecoder(&raw).Decode(&current); err != nil {
		t.Fatal(err)
	}
	if current.Skills != nil || current.Bios != nil || current.SkillLive != nil ||
		current.NG1 != nil || current.People != nil || current.Skills18[id] != want {
		t.Fatal("v18 save retained legacy data or lost the migrated skills")
	}
}

func TestMedicalV18LegacySkillFixtureMigrates(t *testing.T) {
	data, err := os.ReadFile("testdata/medical_v18_legacy.sav")
	if err != nil {
		t.Fatal(err)
	}
	g, info, err := Load(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("load medical v18 legacy payload: %v", err)
	}
	if info.Version != 18 || !g.hasSetup() || len(g.pawnIDs) == 0 {
		t.Fatalf("medical v18 fixture header/setup: %+v", info)
	}
	id := g.pawnIDs[0]
	if got, want := g.skillOf(id, skill.Melee), (skill.Skill{
		Level: 13, XP: 12345, Carry: 12, Passion: skill.Major,
	}); got != want {
		t.Fatalf("medical v18 melee skill %+v, want %+v", got, want)
	}
	if got := g.skillOf(id, skill.Artistic); got.Level != skill.Neutral {
		t.Fatalf("new Artistic skill %+v, want neutral", got)
	}
	if len(g.ng.setup.Colonists) == 0 ||
		g.ng.setup.Colonists[0].Skills[skill.Artistic].Level != skill.Neutral {
		t.Fatal("medical v18 setup colonist skills were not expanded")
	}

	// A re-save uses only the canonical v18 fields and marks that schema,
	// so the next load must not run the historical converter again.
	var buf bytes.Buffer
	if err := g.Save(&buf, "canonical-v18"); err != nil {
		t.Fatal(err)
	}
	zr, err := savefmt.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	dec := gob.NewDecoder(zr)
	var savedInfo SaveInfo
	if err := dec.Decode(&savedInfo); err != nil {
		t.Fatal(err)
	}
	var saved saveState
	if err := savefmt.Finish(dec.Decode(&saved), zr); err != nil {
		t.Fatal(err)
	}
	if !saved.SkillSchema18 || saved.Bios != nil || saved.Skills != nil ||
		saved.SkillLive != nil || saved.NG1 != nil || saved.People != nil {
		t.Fatal("canonical v18 save retained legacy fields or omitted its marker")
	}
	h, info2, err := Load(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("load canonical v18 re-save: %v", err)
	}
	if info2.Version != 18 || h.StateHash() != g.StateHash() {
		t.Fatal("medical v18 migration did not survive a canonical save/load")
	}
}
