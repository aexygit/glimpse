package grading

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/aexygit/glimpse/internal/gear"
)

func rulesJSON(weight string) string {
	return `{"class":"assassin","weights":{"attack":` + weight + `},"bis":{},"thresholds":{"S":0.95}}`
}

func embeddedFS() fstest.MapFS {
	return fstest.MapFS{"assassin.json": {Data: []byte(rulesJSON("1.0"))}}
}

func TestLoadRulesEmbedded(t *testing.T) {
	t.Parallel()
	rules, errs := LoadRules(embeddedFS(), "")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r, ok := rules[gear.Class("assassin")]
	if !ok {
		t.Fatalf("assassin missing: %v", rules)
	}
	if r.Weights["attack"] != 1.0 || r.Thresholds["S"] != 0.95 {
		t.Errorf("bad rules: %+v", r)
	}
}

func TestLoadRulesOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "assassin.json"), []byte(rulesJSON("2.5")), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, errs := LoadRules(embeddedFS(), dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := rules["assassin"].Weights["attack"]; got != 2.5 {
		t.Errorf("weight = %v, want 2.5", got)
	}
}

func TestLoadRulesInvalidOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "assassin.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, errs := LoadRules(embeddedFS(), dir)
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1: %v", len(errs), errs)
	}
	if got := rules["assassin"].Weights["attack"]; got != 1.0 {
		t.Errorf("weight = %v, want embedded 1.0", got)
	}
}

func TestLoadRulesMissingOverrideDir(t *testing.T) {
	t.Parallel()
	rules, errs := LoadRules(embeddedFS(), filepath.Join(t.TempDir(), "nope"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, ok := rules["assassin"]; !ok {
		t.Error("embedded rules missing")
	}
}
