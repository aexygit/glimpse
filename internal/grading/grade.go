package grading

import (
	"cmp"
	"slices"

	"github.com/aexygit/glimpse/internal/gear"
)

type Reason string

const (
	ReasonMissing Reason = "missing"
	ReasonTier    Reason = "tier"
	ReasonEnchant Reason = "enchant"
	// ReasonItem is the slot being at BIS tier and enchant but a weaker item.
	ReasonItem Reason = "item"
)

const maxUpgrades = 3

// minEnchantRatio is the share of its value an unenchanted item keeps. It and the
// linear ramp to BIS below are a tuning knob, not a game fact: the wire carries the
// enchant level but not what it adds, so the curve is a guess the maintainer retunes.
const minEnchantRatio = 0.6

type SlotGrade struct {
	Slot   gear.Slot
	Score  float64
	Letter string
}

type Upgrade struct {
	Slot   gear.Slot
	Gain   float64
	Reason Reason
	// Target is the BIS tier or enchant for those reasons, else 0.
	Target int
}

type Report struct {
	Overall  float64
	Letter   string
	Slots    []SlotGrade
	Upgrades []Upgrade
}

// Weighted sums stat * weight over s.
func Weighted(s gear.Stats, w map[gear.Stat]float64) float64 {
	var sum float64
	for stat, v := range s {
		sum += v * w[stat]
	}
	return sum
}

// enchantRatio scales an item's value by how close its enchant is to BIS.
func enchantRatio(have, bis int) float64 {
	if bis <= 0 {
		return 1
	}
	return minEnchantRatio + (1-minEnchantRatio)*float64(min(have, bis))/float64(bis)
}

// Grade returns false when the character's class has no rules.
func Grade(c gear.Character, rules map[gear.Class]Rules) (Report, bool) {
	r, ok := rules[c.Class]
	if !ok {
		return Report{}, false
	}

	bisValue := map[gear.Slot]float64{}
	var total float64
	for slot, b := range r.BIS {
		if v := Weighted(b.Stats, r.Weights); v > 0 {
			bisValue[slot] = v
			total += v
		}
	}

	rep := Report{Slots: []SlotGrade{}, Upgrades: []Upgrade{}}
	if total > 0 {
		for slot, bv := range bisValue {
			score, reason, target := scoreSlot(c.Items, slot, r, bv)
			share := bv / total
			rep.Overall += score * share
			rep.Slots = append(rep.Slots, SlotGrade{Slot: slot, Score: score, Letter: letter(score, r.Thresholds)})
			if gain := (1 - score) * share; gain > 0 {
				rep.Upgrades = append(rep.Upgrades, Upgrade{Slot: slot, Gain: gain, Reason: reason, Target: target})
			}
		}
	}
	rep.Letter = letter(rep.Overall, r.Thresholds)

	slices.SortFunc(rep.Slots, func(a, b SlotGrade) int { return cmp.Compare(a.Slot, b.Slot) })
	slices.SortFunc(rep.Upgrades, func(a, b Upgrade) int {
		if d := cmp.Compare(b.Gain, a.Gain); d != 0 {
			return d
		}
		return cmp.Compare(a.Slot, b.Slot)
	})
	if len(rep.Upgrades) > maxUpgrades {
		rep.Upgrades = rep.Upgrades[:maxUpgrades]
	}
	return rep, true
}

// scoreSlot returns the capped slot score and the reason/target for an upgrade.
func scoreSlot(items map[gear.Slot]gear.Item, slot gear.Slot, r Rules, bisValue float64) (float64, Reason, int) {
	it, has := items[slot]
	if !has {
		return 0, ReasonMissing, 0
	}
	b := r.BIS[slot]
	// Enchant is the only per-item quality the wire carries besides the id, so it has
	// to move the score: otherwise a +0 best-in-slot item grades S with nothing to fix.
	score := max(min(Weighted(it.Stats, r.Weights)/bisValue*enchantRatio(it.Enchant, b.Enchant), 1), 0)
	switch {
	case it.Tier < b.Tier:
		return score, ReasonTier, b.Tier
	case it.Enchant < b.Enchant:
		return score, ReasonEnchant, b.Enchant
	}
	return score, ReasonItem, 0
}

// letter picks the highest threshold <= score, else "D".
func letter(score float64, thresholds map[string]float64) string {
	best, bestT := "D", -1.0
	for l, t := range thresholds {
		if t <= score && t > bestT {
			best, bestT = l, t
		}
	}
	return best
}
