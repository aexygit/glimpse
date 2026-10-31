package grading

import (
	"io/fs"
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

// errFS fails every Open, which is how fs.ReadDir surfaces a directory it cannot read.
type errFS struct{ err error }

func (f errFS) Open(string) (fs.File, error) { return nil, f.err }

func TestLoadRulesUnreadableOverrideDir(t *testing.T) {
	t.Parallel()
	// Spec line 142 promises a warning for a broken override, so an unreadable
	// directory must not be silently skipped the way a missing one is.
	rules, errs := LoadRules(embeddedFS(), "")
	if len(errs) != 0 {
		t.Fatalf("embedded only: %v", errs)
	}
	if errs := loadDir(errFS{fs.ErrPermission}, rules); len(errs) != 1 {
		t.Errorf("unreadable dir: errs = %v, want exactly 1", errs)
	}
	if errs := loadDir(errFS{fs.ErrNotExist}, rules); len(errs) != 0 {
		t.Errorf("missing dir: errs = %v, want none", errs)
	}
	if _, ok := rules["assassin"]; !ok {
		t.Error("embedded rules lost")
	}
}
