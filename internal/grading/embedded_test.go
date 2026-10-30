package grading

import (
	"testing"

	gradingdata "github.com/aexygit/glimpse/data/grading"
)

func TestEmbeddedRulesValid(t *testing.T) {
	t.Parallel()
	rules, errs := LoadRules(gradingdata.FS, "")
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	b := rules["Assassin"].BIS["MainHand"]
	if b.Tier != 5 || b.Stats["WeaponFixingDamage"] != 1200 {
		t.Errorf("bad weapon BIS: %+v", b)
	}
}
