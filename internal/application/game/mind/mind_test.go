package mind

import (
	"testing"

	"rimworld-go/internal/domain/ai/job"
	"rimworld-go/internal/domain/pawn/mentalbreak"
	"rimworld-go/internal/domain/sim/clock"
	"rimworld-go/internal/domain/sim/entity"
)

// toCheck moves the clock to id's next hourly break check.
func toCheck(f *fakeHost, id entity.EntityID) {
	f.now++
	for (f.now+uint64(id)*checkStagger)%clock.TicksPerHour != 0 {
		f.now++
	}
}

func TestNoBreakAtOrAboveThreshold(t *testing.T) {
	f := newFake(1, 2, 3)
	m := New(f)
	f.mood[1] = mentalbreak.DefaultThresholds().Minor // at the threshold: none
	for range 200 {
		toCheck(f, 1)
		if m.Tick(1, job.Record{}) {
			t.Fatal("broke at the minor threshold")
		}
	}
	if f.ri != 0 {
		t.Fatalf("rolled %d times with no risk", f.ri)
	}
}

func TestOnlyTheHourlyCheckRolls(t *testing.T) {
	f := newFake(1)
	m := New(f)
	f.mood[1] = 0
	f.now = 1
	for (f.now+checkStagger)%clock.TicksPerHour == 0 {
		f.now++
	}
	if m.Tick(1, job.Record{}) || f.ri != 0 || f.awakeCalls != 0 {
		t.Fatal("a break check off the hourly tick")
	}
}

func TestRiskFollowsTheTierMTB(t *testing.T) {
	f := newFake(1, 2, 3)
	m := New(f)
	f.mood[1] = 30 // minor tier: MTB 4 days
	f.rolls = []uint32{clock.TicksPerHour}
	toCheck(f, 1)
	if m.Tick(1, job.Record{}) {
		t.Fatal("a roll at the hour mark must not break")
	}
	f.rolls = []uint32{uint32(mentalbreak.MTBTicks(mentalbreak.Minor)) + clock.TicksPerHour - 1, 0, 0}
	f.ri = 0
	toCheck(f, 1)
	if !m.Tick(1, job.Record{}) {
		t.Fatal("a roll just under the hour (mod MTB) breaks")
	}
	if k, until, _ := f.Break(1); k != mentalbreak.SadWander || until != f.now+mentalbreak.DefOf(k).MinDuration {
		t.Fatalf("break %v until %d", k, until)
	}
	if f.seized != 1 || len(f.started) != 1 {
		t.Fatal("a break seizes the colonist and is announced")
	}
}

func TestThresholdsFollowTheTraitShift(t *testing.T) {
	f := newFake(1, 2, 3)
	m := New(f)
	f.shift[1] = -6 // steady
	if th := m.Thresholds(1); th.Minor != 29 || th.Major != 16 || th.Extreme != 4 {
		t.Fatalf("steady thresholds %+v", th)
	}
	f.mood[1] = 31 // under the standard 35, over steady's 29
	for range 50 {
		toCheck(f, 1)
		if m.Tick(1, job.Record{}) {
			t.Fatal("a steady colonist at 31 broke")
		}
	}
	f.shift[1] = 6 // brittle: 41
	toCheck(f, 1)
	if !m.Tick(1, job.Record{}) {
		t.Fatal("a brittle colonist at 31 should break on a zero roll")
	}
}

func TestInterruptedBreakDoesNotForceImmunity(t *testing.T) {
	f := newFake(1)
	m := New(f)
	f.mood[1] = 0
	m.Force(1, mentalbreak.SadWander)
	m.End(1)
	if len(f.recovered) != 1 || len(f.naturalRecover) != 0 {
		t.Fatal("forced end must not award catharsis")
	}
	toCheck(f, 1)
	if !m.Tick(1, job.Record{}) {
		t.Fatal("a low mood pawn must remain susceptible to a new break")
	}
}

func TestAllowedKinds(t *testing.T) {
	f := newFake(1, 2)
	m := New(f)
	if m.allowed(1, mentalbreak.InsultingSpree) || !m.allowed(1, mentalbreak.TargetedInsult) {
		t.Fatal("a spree needs two others, a targeted insult one")
	}
	f.colonists = append(f.colonists, 3)
	f.x[3] = 30
	if !m.allowed(1, mentalbreak.InsultingSpree) {
		t.Fatal("two others: a spree is allowed")
	}
	if m.allowed(1, mentalbreak.FoodBinge) || m.allowed(1, mentalbreak.HideInRoom) {
		t.Fatal("no food, nowhere to hide")
	}
	f.peaceful[1] = true
	if m.allowed(1, mentalbreak.Berserk) || m.allowed(1, mentalbreak.MurderousRage) {
		t.Fatal("a pawn incapable of violence never turns violent")
	}
	if !m.allowed(1, mentalbreak.Catatonic) || m.allowed(1, mentalbreak.SocialFight) {
		t.Fatal("catatonic always; a social fight never from mood")
	}
	f.peaceful[1] = false
	f.colonists, f.prisoners = []entity.EntityID{1}, []entity.EntityID{2}
	if !m.allowed(1, mentalbreak.MurderousRage) {
		t.Fatal("a prisoner is a murderous rage target")
	}
}

func TestExtremeTierPeacefulGoesCatatonic(t *testing.T) {
	f := newFake(1, 2)
	m := New(f)
	f.mood[1], f.peaceful[1] = 0, true
	toCheck(f, 1)
	if !m.Tick(1, job.Record{}) {
		t.Fatal("no break at rock bottom")
	}
	if k, _, _ := f.Break(1); k != mentalbreak.Catatonic || f.downs != 1 {
		t.Fatalf("break %v downs %d, want catatonic and down", k, f.downs)
	}
}

func TestMinorBreaksAct(t *testing.T) {
	f := newFake(1)
	m := New(f)
	f.canBinge, f.canHide = true, true
	m.Force(1, mentalbreak.SadWander)
	m.Tick(1, job.Record{})
	if f.wandered != 1 {
		t.Fatal("sad wander wanders")
	}
	m.Tick(1, job.Record{Kind: job.Wander, State: job.Goto})
	if f.wandered != 1 {
		t.Fatal("a wander in progress is kept")
	}
	m.Force(1, mentalbreak.FoodBinge)
	m.Tick(1, job.Record{})
	if f.binged != 1 {
		t.Fatal("food binge eats")
	}
	if !m.Tick(1, job.Record{Kind: job.Eat, State: job.Do}) || f.binged != 1 {
		t.Fatal("an eat job in progress is kept")
	}
	m.Force(1, mentalbreak.HideInRoom)
	m.Tick(1, job.Record{})
	if f.hid != 1 {
		t.Fatal("hide in room hides")
	}
}

// TestBreakLastsItsMinimum: a break holds for its duration whatever
// happens to mood, then ends with catharsis (Recovered).
func TestBreakLastsItsMinimum(t *testing.T) {
	f := newFake(1)
	m := New(f)
	m.Force(1, mentalbreak.SadWander)
	_, until, _ := f.Break(1)
	f.mood[1] = 100
	for f.now = 0; f.now < until; f.now += 500 {
		if !m.Tick(1, job.Record{}) {
			t.Fatalf("break ended at %d before %d", f.now, until)
		}
	}
	f.now = until
	if m.Tick(1, job.Record{}) || len(f.naturalRecover) != 1 {
		t.Fatal("the break ends naturally at its time with recovery")
	}
	if _, _, ok := f.Break(1); ok {
		t.Fatal("record left behind")
	}
}

func TestForceSkipsTheDead(t *testing.T) {
	f := newFake(1)
	m := New(f)
	f.dead[1] = true
	m.Force(1, mentalbreak.Berserk)
	if _, _, ok := f.Break(1); ok {
		t.Fatal("the dead don't break")
	}
}

func TestSaveRoundTripAndTarget(t *testing.T) {
	f := newFake(1, 2)
	m := New(f)
	m.Force(1, mentalbreak.TargetedInsult)
	if m.Target(1) != 2 || m.Target(2) != 0 {
		t.Fatalf("targets %d %d", m.Target(1), m.Target(2))
	}
	s := m.Export()
	s.Calm = map[entity.EntityID]uint64{2: 77} // legacy save field
	n := New(f)
	n.Import(s)
	if n.Target(1) != 2 || len(n.Export().Calm) != 0 {
		t.Fatal("legacy calm immunity must not survive load")
	}
	n.Import(Save{}) // an older save
	if n.Target(1) != 0 {
		t.Fatal("an empty save clears the state")
	}
	m.Force(1, mentalbreak.SadWander)
	if m.Target(1) != 0 {
		t.Fatal("an untargeted break names no target")
	}
}

func TestEndAndEatJobKept(t *testing.T) {
	f := newFake(1)
	m := New(f)
	m.Force(1, mentalbreak.Berserk)
	if !m.Tick(1, job.Record{Kind: job.Eat, State: job.Do}) || len(f.assaults) != 0 {
		t.Fatal("a starving berserker still eats")
	}
	m.End(1)
	if _, _, ok := f.Break(1); ok || len(f.recovered) != 1 || len(f.naturalRecover) != 0 {
		t.Fatal("arrest must interrupt without catharsis")
	}
	m.End(1) // nothing to end
	if len(f.recovered) != 1 {
		t.Fatal("ending twice")
	}
}

func TestSleepingPawnSkipsBreakAndInspirationChecks(t *testing.T) {
	f := newFake(1, 2)
	m := New(f)
	f.mood[1] = 0
	f.sleeping[1] = true
	toCheck(f, 1)
	if m.Tick(1, job.Record{Kind: job.Sleep, State: job.Do}) {
		t.Fatal("a sleeping pawn started a mental break")
	}
	if f.ri != 0 || len(f.started) != 0 {
		t.Fatalf("sleeping pawn used break rolls: %d, %+v", f.ri, f.started)
	}
	f.sleeping[1] = false
	toCheck(f, 1)
	if !m.Tick(1, job.Record{}) || len(f.started) == 0 {
		t.Fatal("awake pawn did not resume mental break checks")
	}
}

func BenchmarkSleepingMentalCheck(b *testing.B) {
	f := newFake(1)
	m := New(f)
	f.sleeping[1] = true
	f.mood[1] = 0
	f.now = clock.TicksPerHour - checkStagger
	j := job.Record{Kind: job.Sleep, State: job.Do}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Tick(1, j)
		if _, _, ok := f.Break(1); ok {
			m.End(1)
		}
	}
}
