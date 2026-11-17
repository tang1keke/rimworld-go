package faction

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

// giftCumulative interpolates published Core gift costs when starting at
// zero goodwill (wiki: Factions, Goodwill from gifts). The public examples
// give 2,041 silver for +40, 3,661 for +60 and 5,539 for +75. Between
// those anchors, the curves are our own monotone integer interpolation.
func giftCumulative(gw int) int {
	switch {
	case gw <= 0:
		return GiftSilverPerGoodwill * gw
	case gw <= 40:
		return GiftSilverPerGoodwill*gw + (441*gw*(gw-1)+780)/1560
	case gw <= 60:
		n := gw - 40
		return 2041 + 62*n + (380*n*(n-1)+190)/380
	case gw <= 75:
		n := gw - 60
		return 3661 + 101*n + (363*n*(n-1)+105)/210
	default:
		return 5539 + 160*(gw-75)
	}
}

// GiftCostAt is the silver required for the next goodwill point. Prices
// at negative goodwill and 75+ are the published 40 and 160; intermediate
// prices are based on published cumulative gift examples.
func GiftCostAt(goodwill int) int {
	return giftCumulative(goodwill+1) - giftCumulative(goodwill)
}

// GiftGoodwill returns the goodwill gained by gifting value to faction f.
// When moving back toward a faction's natural range, every fourth paid
// point receives one additional goodwill (wiki: Goodwill 25% bonus).
func GiftGoodwill(f Faction, value int) int {
	if value <= 0 || !f.Type.HasWorldSettlements() || def(f.Type).Fixed || f.Defeated {
		return 0
	}
	gw, paid := f.Goodwill, 0
	for gw < goodwillMax && value >= GiftCostAt(gw) {
		value -= GiftCostAt(gw)
		paid++
		gw++
		if paid%4 == 0 && gw <= def(f.Type).GoodwillMin && gw < goodwillMax {
			gw++
		}
	}
	return gw - f.Goodwill
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
