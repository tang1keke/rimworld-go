package game

import (
	"rimworld-go/internal/domain/pawn/health"
	"rimworld-go/internal/domain/pawn/mood"
	"rimworld-go/internal/domain/sim/entity"
)

// Mood approaches its target every 250 ticks and is saved per pawn.
const (
	moodPeriod  = 250
	moodStagger = 13
)

// socialThoughts contributes prisoner, bond, and partner mood effects.
func (g *Game) socialThoughts(id entity.EntityID) []mood.Thought {
	out := g.prisonerThoughts(id)            // prisoners.go
	out = append(out, g.bondThoughts(id)...) // bonds.go
	if g.soc == nil {
		return out
	}
	p := g.partnerOf(id) // social_romance.go
	if p == 0 {
		return out
	}
	if v := partnerMood(g.opinion(id, p)); v != 0 {
		out = append(out, mood.Thought{Kind: mood.PartnerOpinion, Label: mood.DefOf(mood.PartnerOpinion).Label, Value: v})
	}
	if g.sleptAlone(id, p) {
		d := mood.DefOf(mood.SleptAlone)
		out = append(out, mood.Thought{Kind: mood.SleptAlone, Label: d.Label, Value: d.Value})
	}
	return out
}

// partnerMood clamps the partner-opinion effect to +/-partnerMoodCap.
func partnerMood(opinion int32) int {
	return max(-partnerMoodCap, min(partnerMoodCap, int(opinion/1000)))
}

const partnerMoodCap = 10

// sleptAlone compares the partners' most recent beds while both are on-map.
func (g *Game) sleptAlone(id, p entity.EntityID) bool {
	if !g.onMap(p) || !g.onMap(id) {
		return false
	}
	mine, ok := g.sleptAt[id]
	if !ok {
		return false // not slept yet
	}
	theirs, ok := g.sleptAt[p]
	return !ok || mine == 0 || mine != theirs
}

func (g *Game) smoothedMood(id entity.EntityID) (int, bool) {
	if g.soc != nil {
		if m, ok := g.soc.moodM[id]; ok {
			return int((m + 500) / 1000), true
		}
	}
	return 0, false
}

// stepMood prunes memories and moves an awake pawn's mood toward its target.
func (g *Game) stepMood(id entity.EntityID) {
	v, ok := g.vital.Get(id)
	if !ok || v.Status == health.Dead {
		if g.soc != nil {
			delete(g.soc.moodM, id)
		}
		return
	}
	if m := g.memories[id].Prune(uint64(g.clock.Now())); len(m) > 0 {
		g.memories[id] = m
	} else {
		delete(g.memories, id)
	}
	if g.asleep(id) || v.Status == health.Downed && !g.lyingAwake(id) {
		return
	}
	target := int32(g.thoughtLevel(id)) * 1000
	cur, ok := g.socState().moodM[id]
	if !ok {
		cur = target
	}
	g.soc.moodM[id] = mood.Approach(cur, target, moodPeriod)
}
