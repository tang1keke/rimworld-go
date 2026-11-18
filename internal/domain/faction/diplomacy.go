package faction

import "math"

// Diplomacy rules. Costs from wiki: Comms console. The Trade page still
// lists an older 15-goodwill caravan price; this uses the detailed console
// entry. The arrival delay is documented on wiki: Factions.
const (
	// GiftSilverPerGoodwill is the published *minimum* gift cost, at
	// goodwill below zero; costs rise as goodwill approaches alliance.
	GiftSilverPerGoodwill = 40
	AidCost               = 31
	AidCooldownDays       = 1
	CaravanCost           = 19
	CaravanArriveMinHours = 48
	CaravanArriveMaxHours = 48
	CaravanCooldownDays   = 4
)

// Player-facing refusal reasons.
const (
	ReasonGone       = "that faction is gone"
	ReasonBesieged   = "they refuse anything from us while we attack them"
	ReasonHostile    = "they are hostile"
	ReasonNotFriend  = "they are not friendly enough"
	ReasonGoodwill   = "they don't trust you enough"
	ReasonCoolingOff = "they answered recently"
	ReasonNoFighters = "they have no fighters to send"
)

// Cooldowns is when a faction answers requests again: the tick each
// cooldown ends (0: none). Saved by the game, per faction.
type Cooldowns struct {
	AidUntil     uint64
	CaravanUntil uint64
}

// giftPositiveGain is an independently fitted approximation of the
// Core goodwill curve from 0 to +75, based on public in-game measurements
// (0->40: 2041, 0->60: 3661, 0->75: 5539 silver).
// The model is (100-current)*(1-exp(-silver/3995)); it is a fitted
// curve, not a reproduction of the original game's implementation.
func giftPositiveGain(current, silver int) int {
	if silver <= 0 || current >= goodwillMax {
		return 0
	}
	return int(float64(100-current) * (1 - math.Exp(-float64(silver)/3995)))
}

// GiftCostAt estimates the base price of the next point, excluding any
// natural-goodwill bonus. Below 0 it is 40 silver; 75+ it is 160.
func GiftCostAt(goodwill int) int {
	switch {
	case goodwill < 0:
		return GiftSilverPerGoodwill
	case goodwill >= 75:
		return 160
	default:
		return int(math.Ceil(3995 * math.Log(float64(100-goodwill)/float64(99-goodwill))))
	}
}

// baseGiftGoodwill is the design baseline for an additional 25% bonus
// when goodwill increases toward a faction's base. Natural *bands* are
// independent. Core base-goodwill overrides are not represented as state.
func baseGiftGoodwill(t Type) int {
	if def(t).GoodwillMax < 0 {
		return def(t).GoodwillMax // rough groups: authored -80 baseline
	}
	return 0 // ordinary civil groups
}

// GiftGoodwill converts a single gift's value into goodwill. All of the
// gift is evaluated together, so splitting it may lose rounded points.
// The published flat 40/160-silver ranges join the measured fitted curve
// from 0 to +75. Bonus is +25% for raw goodwill moving toward base.
func GiftGoodwill(f Faction, value int) int {
	if value <= 0 || !f.Type.HasWorldSettlements() || def(f.Type).Fixed || f.Defeated {
		return 0
	}
	start := f.Goodwill
	gw := start
	if gw < 0 {
		paid := min(-gw, value/GiftSilverPerGoodwill)
		gw += paid
		value -= paid * GiftSilverPerGoodwill
	}
	if gw >= 0 && gw < 75 && value > 0 {
		// Find the exact integer value needed to reach +75 using the
		// same fitted function as the partial-gift branch.
		lo, hi := 0, 6000
		for lo < hi {
			mid := lo + (hi-lo)/2
			if giftPositiveGain(gw, mid) >= 75-gw {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		if value >= lo {
			gw = 75
			value -= lo
		} else {
			gw += giftPositiveGain(gw, value)
			value = 0
		}
	}
	if gw >= 75 {
		gw += value / 160
	}
	base := baseGiftGoodwill(f.Type)
	if start < base {
		towardBase := min(gw, base) - start
		if towardBase > 0 {
			gw += towardBase / 4
		}
	}
	return min(gw, goodwillMax) - start
}

// GiftSilverFor is the minimum gift value needed to attain target goodwill.
// It uses the same gift projection as the actual trade, radio and pod paths.
func GiftSilverFor(f Faction, target int) int {
	if target <= f.Goodwill || !f.Type.HasWorldSettlements() || def(f.Type).Fixed || f.Defeated {
		return 0
	}
	target = min(target, goodwillMax)
	delta := target - f.Goodwill
	lo, hi := 0, (goodwillMax-goodwillMin)*160
	for lo < hi {
		mid := lo + (hi-lo)/2
		if GiftGoodwill(f, mid) >= delta {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// TradeGoodwill accrues +1 per 500 silver of merchandise traded
// (wiki: Factions). The fractional carry lives in the saved faction and
// includes both sides of a successful physical transaction. Gift exchanges
// have their own goodwill accounting and do not enter this counter.
func TradeGoodwill(f *Faction, value int, event string) int {
	if f == nil || value <= 0 || f.Defeated || f.Besieged ||
		!f.Type.HasWorldSettlements() || def(f.Type).Fixed {
		return 0
	}
	total := int(f.TradeRemainder) + value
	delta := min(total/500, goodwillMax-f.Goodwill)
	f.TradeRemainder = uint16(total % 500)
	if delta > 0 {
		Adjust(f, delta, event)
	}
	return delta
}

// CanRequestAid reports whether f answers a military aid request at tick
// now, or the player-facing reason it won't.
func CanRequestAid(f Faction, cd Cooldowns, now uint64) (bool, string) {
	switch {
	case f.Defeated:
		return false, ReasonGone
	case f.Relation == Hostile:
		return false, ReasonHostile
	case f.Relation != Friendly:
		return false, ReasonNotFriend
	case !f.Type.HasWorldSettlements() || def(f.Type).Tech == Neolithic:
		return false, ReasonNoFighters
	case f.Goodwill < AidCost:
		return false, ReasonGoodwill
	case now < cd.AidUntil:
		return false, ReasonCoolingOff
	}
	return true, ""
}

// CanRequestCaravan reports whether f sends a trade caravan on request at
// tick now, or the player-facing reason it won't.
func CanRequestCaravan(f Faction, cd Cooldowns, now uint64) (bool, string) {
	switch {
	case f.Defeated:
		return false, ReasonGone
	case f.Relation == Hostile:
		return false, ReasonHostile
	case !f.Type.HasWorldSettlements():
		return false, ReasonNotFriend
	case f.Relation != Friendly:
		return false, ReasonNotFriend
	case f.Goodwill < CaravanCost:
		return false, ReasonGoodwill
	case now < cd.CaravanUntil:
		return false, ReasonCoolingOff
	}
	return true, ""
}
