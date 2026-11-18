package trade

import (
	"rimworld-go/internal/domain/building/construction"
	"rimworld-go/internal/domain/economy/item"
)

// GiftMethod distinguishes visiting-trader offers and unaccompanied
// transport pods. Both follow the public Core sell-price rules.
type GiftMethod uint8

const (
	GiftVisitor GiftMethod = iota
	GiftPod
)

// giftRate is the percentage of market value in basis points.
// Visiting gifts use 0.6*(1+Social*1.5%); unaccompanied pods use 0.612.
func giftRate(method GiftMethod, social int) int {
	if method == GiftPod {
		return 6120
	}
	return 6000 + 90*min(max(social, 0), 20)
}

// GiftBaseCenti values ordinary goods in hundredths of a silver.
// wiki: Factions, Goodwill from gifts; Trade Price Improvement.
func GiftBaseCenti(market int, method GiftMethod, social int) int {
	if market <= 0 {
		return 0
	}
	return (market*giftRate(method, social) + 50) / 100
}

// GiftUnitCenti applies the kind's sell-price multiplier to one item.
// Silver keeps face value; weapons sell for one fifth of normal value.
// Minified buildings sent by pod retain the normal multiplier (wiki).
func GiftUnitCenti(k item.Kind, market int, method GiftMethod, social int) int {
	if market <= 0 {
		return 0
	}
	if k == item.Silver {
		return market * 100
	}
	if k.IsWeapon() {
		return (market*giftRate(method, social)/5 + 50) / 100
	}
	return GiftBaseCenti(market, method, social)
}

// GiftGoodsCenti evaluates a keyed line in gift mode. A minified building
// gifted through a trader has the published furniture/sculpture sell
// modifier; pod delivery uses the building's full market coefficient.
func GiftGoodsCenti(g Goods, method GiftMethod, social int) int {
	market := g.Value()
	value := GiftUnitCenti(g.Kind, market, method, social)
	if g.Kind != item.MinifiedBuilding || method == GiftPod {
		return value
	}
	switch construction.Kind(g.Building) {
	case construction.SmallSculpture, construction.LargeSculpture, construction.GrandSculpture:
		return (value*110 + 50) / 100
	default:
		return (value*70 + 50) / 100
	}
}
