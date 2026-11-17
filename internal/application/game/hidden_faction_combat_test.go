package game

import (
	"testing"

	"rimworld-go/internal/application/game/raid"
	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/animal"
	"rimworld-go/internal/domain/economy/item"
	"rimworld-go/internal/domain/faction"
	"rimworld-go/internal/domain/sim/entity"
	"rimworld-go/internal/domain/world/terrain"
)

func hiddenBattle(t *testing.T) (*Game, entity.EntityID) {
	t.Helper()
	g := New(Config{Seed: 91, Width: 80, Height: 45})
	_, err := g.SpawnNamedPawn(72, 40, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	g.raidWith([]item.Kind{item.Club}, raid.Assault)
	if len(g.raiderIDs) != 1 {
		t.Fatalf("raiders %v", g.raiderIDs)
	}
	rid := g.raiderIDs[0]
	g.positions.Set(rid, Position{X: 10, Y: 10})
	return g, rid
}

func hiddenSpawn(t *testing.T, g *Game, s animal.Species, x, y int) entity.EntityID {
	t.Helper()
	id, err := g.SpawnAnimal(s.String(), x, y)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestHiddenBattleHumanRaiderSelectsMech(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 12, 10)
	g.raid.Raiders.Think(rid, uint64(g.clock.Now()))
	if j, _ := g.jobs.Get(rid); j.Kind != job.Attack || j.Target != mech {
		t.Fatalf("human raider ignores nearby hostile mech: %+v", j)
	}
}

func TestHiddenBattleMechSelectsHumanAndInsect(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 13, 10)
	insect := hiddenSpawn(t, g, animal.HiveCrawler, 14, 10)
	a, _ := g.animals.Get(mech)
	now := uint64(g.clock.Now())
	for i := uint64(0); i < animal.DecideEvery && a.Target == 0; i++ {
		g.raid.Mechs.Think(mech, &a, now+i)
	}
	if a.Target != insect {
		t.Fatalf("machine ignores nearer insect: target %d, want %d", a.Target, insect)
	}
	g.KillAnimal(insect)
	a.Target = 0
	for i := uint64(0); i < animal.DecideEvery && a.Target == 0; i++ {
		g.raid.Mechs.Think(mech, &a, now+animal.DecideEvery+i)
	}
	if a.Target != rid {
		t.Fatalf("machine ignores human raider: target %d, want %d", a.Target, rid)
	}
}

func TestHiddenBattleHiveSelectsMech(t *testing.T) {
	t.Parallel()
	g, _ := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 13, 10)
	hive := hiddenSpawn(t, g, animal.HiveCrawler, 14, 10)
	a, _ := g.animals.Get(hive)
	a.Manhunter, a.Mode = raid.HiveForever, animal.Attack
	g.thinkAnimal(hive, &a, 1)
	if a.Target != mech {
		t.Fatalf("hive insect ignores nearer mech: target %d, want %d", a.Target, mech)
	}
}

func TestHiddenBattleLOSInactiveAndReplay(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 12, 10)
	inactive := hiddenSpawn(t, g, animal.Spindle, 11, 11)
	a, _ := g.animals.Get(inactive)
	a.Dormant = true
	g.animals.Set(inactive, a)
	g.setTerrain(11, 10, terrain.Rock)
	if g.combat.Sight.Line(10, 10, 12, 10) {
		t.Fatal("fixture does not block direct visibility")
	}
	g.raid.Raiders.Think(rid, uint64(g.clock.Now()))
	if j, _ := g.jobs.Get(rid); j.Kind == job.Attack && (j.Target == mech || j.Target == inactive) {
		t.Fatalf("raider selected blocked or dormant opponent: %+v", j)
	}
	g.setTerrain(11, 10, terrain.Soil)
	for i := 1; i <= 30; i++ {
		g.raid.Raiders.Think(rid, uint64(g.clock.Now())+uint64(i))
		if j, _ := g.jobs.Get(rid); j.Kind == job.Attack && j.Target == mech {
			break
		}
	}
	if j, _ := g.jobs.Get(rid); j.Kind != job.Attack || j.Target != mech {
		t.Fatalf("raider ignored now-visible active opponent: %+v", j)
	}
	h := saveLoad(t, g)
	j, _ := h.jobs.Get(rid)
	if j.Target != mech {
		t.Fatalf("saved targeting lost faction foe: %+v", j)
	}
	for i := 0; i < 20; i++ {
		g.Step(1)
		h.Step(1)
	}
	if g.StateHash() != h.StateHash() {
		t.Fatal("hidden cross-faction combat diverged on replay")
	}
}

func TestHiddenBattleActualDamage(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 11, 10)
	hit := false
	for i := 0; i < 450; i++ {
		g.Step(1)
		for _, tr := range g.traces {
			if (tr.Attacker == rid && tr.Target == mech || tr.Attacker == mech && tr.Target == rid) && tr.Damage > 0 {
				hit = true
				break
			}
		}
		if hit {
			break
		}
	}
	if !hit {
		t.Fatal("real human/mech combat dealt no damage")
	}
}

// A live mechanoid belongs to the same hive as its neighbours. Wild
// animals, the mech's own group and dormant defenders are ignored.
func TestHiddenBattleMechKeepsCurrentFoeAcrossNearTie(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	mech := hiddenSpawn(t, g, animal.Harrow, 12, 10)
	friend := hiddenSpawn(t, g, animal.Spindle, 13, 9)
	hive := hiddenSpawn(t, g, animal.HiveCrawler, 14, 10)
	wild := hiddenSpawn(t, g, animal.Deer, 12, 11)
	for _, v := range (raidTactics{g}).Foes(mech, 12, 10, 20, nil) {
		if v.ID == friend || v.ID == wild {
			t.Fatalf("same-faction mech or unrelated wildlife selected: %+v", v)
		}
	}
	a, _ := g.animals.Get(mech)
	a.Target = hive
	now := uint64(g.clock.Now())
	for i := uint64(0); i < animal.DecideEvery; i++ {
		g.raid.Mechs.Think(mech, &a, now+i)
		if a.Target != hive {
			t.Fatalf("mech switched from equal-distance hive to human raider %d: target=%d", rid, a.Target)
		}
	}
}

// Colony actors remain eligible to fight hostile mechanoids, while a
// normal wildlife animal does not gain an inter-faction enemy of its own.
func TestHiddenBattleHumanRaiderTargetsHiveWithoutFriendlyFire(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	hive := hiddenSpawn(t, g, animal.HiveBeetle, 12, 10)
	hiddenSpawn(t, g, animal.Deer, 11, 10)
	g.raid.Raiders.Think(rid, uint64(g.clock.Now()))
	if j, _ := g.jobs.Get(rid); j.Kind != job.Attack || j.Target != hive {
		t.Fatalf("raider's target %+v, want hostile insect %d", j, hive)
	}
}

// A machine and an infestation insect share a map with no nearby people.
// Their regular AI must start a real fight, using normal damage records.
func TestHiddenBattleHiveAndMechFight(t *testing.T) {
	t.Parallel()
	g := New(Config{Seed: 72, Width: 80, Height: 45})
	mech := hiddenSpawn(t, g, animal.Harrow, 10, 10)
	hive := hiddenSpawn(t, g, animal.HiveCrawler, 11, 10)
	a, _ := g.animals.Get(hive)
	a.Manhunter, a.Mode = raid.HiveForever, animal.Attack
	g.animals.Set(hive, a)
	hitMech, hitHive := false, false
	for i := 0; i < 1000 && !(hitMech && hitHive); i++ {
		g.Step(1)
		for _, trace := range g.traces {
			if trace.Damage <= 0 {
				continue
			}
			if trace.Attacker == mech && trace.Target == hive {
				hitHive = true
			}
			if trace.Attacker == hive && trace.Target == mech {
				hitMech = true
			}
		}
	}
	if !hitMech || !hitHive {
		t.Fatalf("machine and hive did not exchange damage: hitMech=%v hitHive=%v", hitMech, hitHive)
	}
}

// A neutral visitor occupies the visitor lifecycle roster, where
// Leaving=true keeps ordinary raider AI away. Hidden factions still
// target that other human faction instead of ignoring it.
func TestHiddenBattleMechTargetsNeutralVisitor(t *testing.T) {
	t.Parallel()
	g := New(Config{Seed: 55, Width: 80, Height: 45})
	f := faction.Faction{ID: 0, Type: faction.Settlers, Name: "North Hearth"}
	g.ng.factions = []faction.Faction{f}
	va := visitArrivals{g}
	visitor := va.NewPerson(1, &g.ng.factions[0])
	va.Place(visitor, 1, 11, 10, uint64(g.clock.Now()))
	if g.standingRaider(visitor) || !g.standingPawn(visitor) {
		t.Fatal("neutral visitor fixture is incorrectly treated as hostile")
	}
	mech := hiddenSpawn(t, g, animal.Harrow, 10, 10)
	a, _ := g.animals.Get(mech)
	now := uint64(g.clock.Now())
	for i := uint64(0); i < animal.DecideEvery && a.Target == 0; i++ {
		g.raid.Mechs.Think(mech, &a, now+i)
	}
	if a.Target != visitor {
		t.Fatalf("mech ignored human visitor %d: target %d", visitor, a.Target)
	}
}

// Raider target scoring weighs ranged threat against distance. Every
// visible foreign faction opponent must reach the ordinary scorer.
func TestHiddenBattleRaiderWeighsAllForeignThreats(t *testing.T) {
	t.Parallel()
	g, rid := hiddenBattle(t)
	near := hiddenSpawn(t, g, animal.Harrow, 12, 10)
	gunner := hiddenSpawn(t, g, animal.Spindle, 15, 10)
	candidates := (raidTactics{g}).Foes(rid, 10, 10, 30, nil)
	foundNear, foundGunner := false, false
	for _, s := range candidates {
		foundNear = foundNear || s.ID == near
		foundGunner = foundGunner || s.ID == gunner
	}
	if !foundNear || !foundGunner {
		t.Fatalf("candidate filter truncated foreign threats: %+v", candidates)
	}
	g.raid.Raiders.Think(rid, uint64(g.clock.Now()))
	if j, _ := g.jobs.Get(rid); j.Kind != job.Attack || j.Target != gunner {
		t.Fatalf("raider ignored ranged threat: %+v, want gunner %d", j, gunner)
	}
}

// A tamed animal belongs to the colony; a hostile mechanoid should
// recognize the faction of the owner as it does for colonists.
func TestHiddenBattleMechTargetsTameColonyAnimal(t *testing.T) {
	t.Parallel()
	g := New(Config{Seed: 36, Width: 80, Height: 45})
	pet := hiddenSpawn(t, g, animal.Labrador, 11, 10)
	g.TameAnimal(pet, "Bo")
	mech := hiddenSpawn(t, g, animal.Harrow, 10, 10)
	a, _ := g.animals.Get(mech)
	now := uint64(g.clock.Now())
	for i := uint64(0); i < animal.DecideEvery && a.Target == 0; i++ {
		g.raid.Mechs.Think(mech, &a, now+i)
	}
	if a.Target != pet {
		t.Fatalf("mech treated colony pet %d as wild animal: target=%d", pet, a.Target)
	}
}

// Core manhunter animals pursue hostile mechanoids as well as people.
// Infestation insects still keep their own permanent-faction rule.
func TestHiddenBattleManhunterTargetsMech(t *testing.T) {
	t.Parallel()
	g := New(Config{Seed: 37, Width: 80, Height: 45})
	mech := hiddenSpawn(t, g, animal.Harrow, 10, 10)
	wolf := hiddenSpawn(t, g, animal.TimberWolf, 11, 10)
	a, _ := g.animals.Get(wolf)
	a.Manhunter, a.Mode = raid.HiveForever, animal.Attack
	g.thinkAnimal(wolf, &a, uint64(g.clock.Now()))
	if a.Target != mech {
		t.Fatalf("manhunter ignored nearby hostile machine %d: target=%d", mech, a.Target)
	}
}
