// Package gamedata caches item metadata and icons on disk.
package gamedata

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/aexygit/glimpse/internal/gear"
)

// Version is set at build time and used in the HTTP User-Agent.
var Version = "dev"

type Entry struct {
	ID    int        `json:"id"`
	Name  string     `json:"name"`
	Tier  int        `json:"tier"`
	Icon  string     `json:"icon"`
	Stats gear.Stats `json:"stats"`
}

// Store is safe for concurrent use.
type Store struct {
	dir   string
	mu    sync.RWMutex
	items map[int]Entry
}

// Open loads cacheDir/items.json, creating cacheDir if needed. A corrupt
// file is renamed to items.json.bad and an empty store is returned.
func Open(cacheDir string) (*Store, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	s := &Store{dir: cacheDir, items: map[int]Entry{}}
	path := s.path()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	uerr := json.Unmarshal(data, &s.items)
	if uerr == nil && s.items == nil {
		uerr = errors.New("items.json is null")
	}
	if uerr != nil {
		s.items = map[int]Entry{}
		if err := os.Rename(path, path+".bad"); err != nil {
			return nil, fmt.Errorf("quarantine corrupt cache: %w", err)
		}
		slog.Default().Warn("corrupt item cache, rebuilding", "error", uerr, "backup", path+".bad")
	}
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "items.json") }

func (s *Store) Get(id int) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.items[id]
	e.Stats = maps.Clone(e.Stats)
	return e, ok
}

// Put persists entries atomically; the in-memory map changes only on success.
func (s *Store) Put(entries ...Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := maps.Clone(s.items)
	for _, e := range entries {
		e.Stats = maps.Clone(e.Stats)
		next[e.ID] = e
	}
	data, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode items: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, "items-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("replace items.json: %w", err)
	}
	s.items = next
	return nil
}

func (s *Store) IconPath(id int) string {
	return filepath.Join(s.dir, "icons", strconv.Itoa(id)+".png")
}
