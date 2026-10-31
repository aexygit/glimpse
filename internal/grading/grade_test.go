package grading

import (
	"math"
	"testing"

	"github.com/aexygit/glimpse/internal/gear"
)

func testRules() map[gear.Class]Rules {
	return map[gear.Class]Rules{"assassin": {
		Class:   "assassin",
		Weights: map[gear.Stat]float64{"attack": 1, "crit": 0.5},
		BIS: map[gear.Slot]BIS{
			"weapon": {Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 100}},
			"ring1":  {Tier: 5, Enchant: 10, Stats: gear.Stats{"crit": 40}},
		},
		Thresholds: map[string]float64{"S": 0.95, "A": 0.85, "B": 0.70, "C": 0.50},
	}}
}

func bisRing() gear.Item {
	return gear.Item{Tier: 5, Enchant: 10, Stats: gear.Stats{"crit": 40}}
}

func char(items map[gear.Slot]gear.Item) gear.Character {
	return gear.Character{Class: "assassin", Items: items}
}

func TestGrade(t *testing.T) {
	bisWeapon := gear.Item{Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 100}}
	tests := []struct {
		name        string
		items       map[gear.Slot]gear.Item
		overall     float64
		letter      string
		weaponScore float64
		upgrades    []Upgrade
	}{
		{
			name:        "full BIS",
			items:       map[gear.Slot]gear.Item{"weapon": bisWeapon, "ring1": bisRing()},
			overall:     1,
			letter:      "S",
			weaponScore: 1,
		},
		{
			name:     "empty",
			items:    nil,
			overall:  0,
			letter:   "D",
			upgrades: []Upgrade{{Slot: "weapon", Reason: ReasonMissing}, {Slot: "ring1", Reason: ReasonMissing}},
		},
		{
			name: "weaker item at BIS tier and enchant",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 50}},
				"ring1":  bisRing(),
			},
			overall:     0.5/1.2 + 1.0/6,
			letter:      "C",
			weaponScore: 0.5,
			upgrades:    []Upgrade{{Slot: "weapon", Reason: ReasonItem}},
		},
		{
			name: "score capped above BIS",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 150}},
				"ring1":  bisRing(),
			},
			overall:     1,
			letter:      "S",
			weaponScore: 1,
		},
		{
			name: "low tier",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 4, Enchant: 15, Stats: gear.Stats{"attack": 100}},
				"ring1":  bisRing(),
			},
			overall:     1,
			letter:      "S",
			weaponScore: 1,
			upgrades:    nil, // full score means no gain, so no advice
		},
		{
			name: "low tier with low stats",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 4, Enchant: 15, Stats: gear.Stats{"attack": 90}},
				"ring1":  bisRing(),
			},
			overall:     0.9*100/120 + 20.0/120,
			letter:      "A",
			weaponScore: 0.9,
			upgrades:    []Upgrade{{Slot: "weapon", Reason: ReasonTier, Target: 5}},
		},
		{
			// +12 of BIS +15 keeps 0.6 + 0.4*12/15 of the item's value.
			name: "low enchant",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 5, Enchant: 12, Stats: gear.Stats{"attack": 90}},
				"ring1":  bisRing(),
			},
			overall:     0.828*100/120 + 20.0/120,
			letter:      "A",
			weaponScore: 0.828,
			upgrades:    []Upgrade{{Slot: "weapon", Reason: ReasonEnchant, Target: 15}},
		},
		{
			// The BIS item at +0 is not best in slot: it must not grade S and must
			// show up as an upgrade, which is what dropping enchant from the score did.
			name: "BIS item unenchanted",
			items: map[gear.Slot]gear.Item{
				"weapon": {Tier: 5, Stats: gear.Stats{"attack": 100}},
				"ring1":  bisRing(),
			},
			overall:     minEnchantRatio*100/120 + 20.0/120,
			letter:      "C",
			weaponScore: minEnchantRatio,
			upgrades:    []Upgrade{{Slot: "weapon", Reason: ReasonEnchant, Target: 15}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, ok := Grade(char(tt.items), testRules())
			if !ok {
				t.Fatal("ok = false")
			}
			if math.Abs(rep.Overall-tt.overall) > 1e-9 || rep.Letter != tt.letter {
				t.Errorf("overall = %v %s, want %v %s", rep.Overall, rep.Letter, tt.overall, tt.letter)
			}
			if len(rep.Slots) != 2 || rep.Slots[0].Slot != "ring1" || rep.Slots[1].Slot != "weapon" {
				t.Fatalf("slots = %+v, want ring1 then weapon", rep.Slots)
			}
			if got := rep.Slots[1].Score; math.Abs(got-tt.weaponScore) > 1e-9 {
				t.Errorf("weapon score = %v, want %v", got, tt.weaponScore)
			}
			if len(rep.Upgrades) != len(tt.upgrades) {
				t.Fatalf("upgrades = %+v, want %+v", rep.Upgrades, tt.upgrades)
			}
			for i, want := range tt.upgrades {
				got := rep.Upgrades[i]
				if got.Slot != want.Slot || got.Reason != want.Reason || got.Target != want.Target || got.Gain <= 0 {
					t.Errorf("upgrade %d = %+v, want %+v with Gain > 0", i, got, want)
				}
			}
		})
	}
}

func TestGradeUpgradesCapAndOrder(t *testing.T) {
	r := testRules()
	rl := r["assassin"]
	rl.BIS = map[gear.Slot]BIS{
		"a": {Stats: gear.Stats{"attack": 10}},
		"b": {Stats: gear.Stats{"attack": 40}},
		"c": {Stats: gear.Stats{"attack": 30}},
		"d": {Stats: gear.Stats{"attack": 20}},
	}
	r["assassin"] = rl
	rep, _ := Grade(char(nil), r)
	var got []gear.Slot
	for _, u := range rep.Upgrades {
		got = append(got, u.Slot)
	}
	want := []gear.Slot{"b", "c", "d"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("upgrades = %v, want %v", got, want)
	}
}

func TestGradeUnknownClass(t *testing.T) {
	if _, ok := Grade(gear.Character{Class: "nope"}, testRules()); ok {
		t.Error("ok = true, want false")
	}
}

func TestGradeZeroWeights(t *testing.T) {
	r := testRules()
	rl := r["assassin"]
	rl.Weights = map[gear.Stat]float64{"attack": 0, "crit": 0}
	r["assassin"] = rl
	c := char(map[gear.Slot]gear.Item{"weapon": {Stats: gear.Stats{"attack": 5}}})
	rep, ok := Grade(c, r)
	if !ok || math.IsNaN(rep.Overall) || rep.Overall != 0 || rep.Letter != "D" {
		t.Errorf("zero weights: %+v ok=%v", rep, ok)
	}

	r = testRules()
	rl = r["assassin"]
	delete(rl.BIS, "ring1")
	r["assassin"] = rl
	c = char(map[gear.Slot]gear.Item{"weapon": {Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 100}}, "ring1": bisRing()})
	rep, _ = Grade(c, r)
	if math.IsNaN(rep.Overall) || rep.Overall != 1 {
		t.Errorf("overall = %v, want 1", rep.Overall)
	}
	for _, s := range rep.Slots {
		if s.Slot == "ring1" {
			t.Error("ring1 should be absent from Slots")
		}
	}
}
