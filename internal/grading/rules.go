// Package grading loads grading rules and scores characters.
package grading

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"

	"github.com/aexygit/glimpse/internal/gear"
)

type BIS struct {
	ItemID  int        `json:"itemId"`
	Tier    int        `json:"tier"`
	Enchant int        `json:"enchant"`
	Stats   gear.Stats `json:"stats"`
}

type Rules struct {
	Class      gear.Class            `json:"class"`
	Weights    map[gear.Stat]float64 `json:"weights"`
	BIS        map[gear.Slot]BIS     `json:"bis"`
	Thresholds map[string]float64    `json:"thresholds"`
}

// LoadRules reads *.json rules from embedded, then lets files in overrideDir
// replace them by class. Bad files are skipped and reported in the error slice.
func LoadRules(embedded fs.FS, overrideDir string) (map[gear.Class]Rules, []error) {
	rules := map[gear.Class]Rules{}
	errs := loadDir(embedded, rules)
	if overrideDir != "" {
		errs = append(errs, loadDir(os.DirFS(overrideDir), rules)...)
	}
	return rules, errs
}

func loadDir(fsys fs.FS, into map[gear.Class]Rules) []error {
	names, err := fs.Glob(fsys, "*.json")
	if err != nil {
		return []error{fmt.Errorf("listing rules: %w", err)}
	}
	var errs []error
	for _, name := range names {
		r, err := loadFile(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		into[r.Class] = r
	}
	return errs
}

func loadFile(fsys fs.FS, name string) (Rules, error) {
	var r Rules
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return r, fmt.Errorf("reading rules %q: %w", name, err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parsing rules %q: %w", name, err)
	}
	if r.Class == "" {
		return r, fmt.Errorf("rules %q: missing class", name)
	}
	return r, nil
}
