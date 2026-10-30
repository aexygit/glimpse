package gamedata

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aexygit/glimpse/internal/gear"
)

func sample(id int) Entry {
	return Entry{ID: id, Name: "Sword", Tier: 3, Icon: "x.png", Stats: gear.Stats{"atk": 12.5}}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "items.json"), []byte("{garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := os.ReadFile(filepath.Join(dir, "items.json.bad"))
	if err != nil || string(bad) != "{garbage" {
		t.Errorf("items.json.bad = %q, %v", bad, err)
	}
	if _, ok := s.Get(1); ok {
		t.Error("store should be empty")
	}
	if err := s.Put(sample(1)); err != nil {
		t.Fatalf("Put after rebuild: %v", err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get(1); !ok {
		t.Error("entry lost after rebuild")
	}
}

func TestStoreStatsNotAliased(t *testing.T) {
	s, _ := Open(t.TempDir())
	in := sample(1)
	if err := s.Put(in); err != nil {
		t.Fatal(err)
	}
	in.Stats["atk"] = 999
	got, _ := s.Get(1)
	if got.Stats["atk"] != 12.5 {
		t.Errorf("Put arg mutation leaked: %v", got.Stats)
	}
	got.Stats["atk"] = 777
	again, _ := s.Get(1)
	if again.Stats["atk"] != 12.5 {
		t.Errorf("Get result mutation leaked: %v", again.Stats)
	}
}

func TestStoreMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("dir not created: %v", err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(sample(7), sample(8)); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(7)
	if !ok || got.Name != "Sword" || got.Stats["atk"] != 12.5 {
		t.Errorf("Get(7) = %+v, %v", got, ok)
	}
	if _, ok := s2.Get(9); ok {
		t.Error("Get(9) should miss")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func TestIconPath(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if got, want := s.IconPath(42), filepath.Join(dir, "icons", "42.png"); got != want {
		t.Errorf("IconPath = %q, want %q", got, want)
	}
}

func TestStoreConcurrent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := s.Put(sample(i)); err != nil {
				t.Error(err)
			}
			s.Get(i)
		})
		wg.Go(func() { s.Get(i) })
	}
	wg.Wait()
	for i := range 20 {
		if _, ok := s.Get(i); !ok {
			t.Errorf("missing %d", i)
		}
	}
}
