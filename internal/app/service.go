// Package app wires capture, game data and grading for the frontend.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aexygit/glimpse/internal/capture"
	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/gear"
	"github.com/aexygit/glimpse/internal/grading"
	"github.com/aexygit/glimpse/internal/protocol"
)

// ponytail: session keeps the last 200 characters in memory; persist to disk if history matters.
const maxSession = 200

// refetchAfter is how long a failed item fetch is remembered. Player repeats for as
// long as a character is in view, so without this an item the catalog does not have
// is re-requested from the API forever.
// ponytail: entries are never swept, only overwritten; bounded by distinct item ids seen.
const refetchAfter = 10 * time.Minute

type Inspected struct {
	// ID identifies one character in the session, not one broadcast.
	ID        int
	Character gear.Character
	Report    grading.Report
	Graded    bool
	Icons     map[gear.Slot]string
}

type Service struct {
	store   *gamedata.Store
	fetcher *gamedata.Fetcher
	rules   map[gear.Class]grading.Rules
	ctx     context.Context
	emit    func(name string, data any)
	paths   map[string]string

	downloading atomic.Bool

	mu       sync.Mutex
	session  []Inspected
	byEntity map[int]int // wire entity id -> Inspected.ID
	nextID   int
	inflight map[int]chan struct{}
	failed   map[int]time.Time // item id -> when its last fetch failed
	warnings []string
	status   capture.Status
}

// New builds a Service. ctx bounds background fetches; emit may be called from any goroutine.
func New(ctx context.Context, store *gamedata.Store, fetcher *gamedata.Fetcher, rules map[gear.Class]grading.Rules,
	paths map[string]string, emit func(string, any)) *Service {
	return &Service{ctx: ctx, store: store, fetcher: fetcher, rules: rules, paths: paths, emit: emit,
		byEntity: map[int]int{}, inflight: map[int]chan struct{}{}, failed: map[int]time.Time{}}
}

func (s *Service) Session() []Inspected {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Inspected, 0, len(s.session))
	return append(out, s.session...)
}

func (s *Service) Status() capture.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// AddWarning records a rules warning and emits it.
func (s *Service) AddWarning(w string) {
	s.mu.Lock()
	s.warnings = append(s.warnings, w)
	s.mu.Unlock()
	s.emit("rules:warning", w)
}

func (s *Service) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.warnings))
	return append(out, s.warnings...)
}

func (s *Service) Paths() map[string]string { return s.paths }

// SetStatus records and emits the capture status.
func (s *Service) SetStatus(st capture.Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.emit("status", st)
}

// DownloadAssets walks the whole catalog. Only one run at a time: a second one
// would double the work and interleave its progress events with the first.
func (s *Service) DownloadAssets() error {
	if !s.downloading.CompareAndSwap(false, true) {
		return errors.New("a download is already running")
	}
	defer s.downloading.Store(false)
	return s.fetcher.All(s.ctx, func(done, total int) {
		s.emit("assets:progress", map[string]int{"done": done, "total": total})
	})
}

// OnMessage handles a decoded capture message. It never blocks on the network.
func (s *Service) OnMessage(m any) {
	r, ok := m.(protocol.InspectResult)
	if !ok {
		return
	}
	in := s.build(r)
	event := "inspect"
	s.mu.Lock()
	// Player is a proximity broadcast that repeats for the same character, so a
	// known entity overwrites its entry instead of adding one.
	if i := s.indexOfEntityLocked(r.Entity); i >= 0 {
		in.ID = s.session[i].ID
		s.session[i] = in
		event = "inspect:update"
	} else {
		s.nextID++
		in.ID = s.nextID
		s.session = append(s.session, in)
		s.byEntity[r.Entity] = in.ID
		s.evictLocked()
	}
	s.mu.Unlock()
	s.emit(event, in)

	if mine, others := s.claimUnknown(in.Character); len(mine)+len(others) > 0 {
		go s.fetchAndUpdate(in.ID, r, mine, others)
	}
}

// indexOfEntityLocked returns the session index holding entity, or -1. s.mu must be held.
func (s *Service) indexOfEntityLocked(entity int) int {
	id, ok := s.byEntity[entity]
	if !ok {
		return -1
	}
	return s.indexOfLocked(id)
}

// indexOfLocked returns the session index of the entry with this ID, or -1. s.mu must be held.
func (s *Service) indexOfLocked(id int) int {
	return slices.IndexFunc(s.session, func(in Inspected) bool { return in.ID == id })
}

// evictLocked drops the oldest character once the session is over the cap. s.mu must be held.
func (s *Service) evictLocked() {
	if len(s.session) <= maxSession {
		return
	}
	gone := s.session[0].ID
	s.session = s.session[1:]
	for entity, id := range s.byEntity {
		if id == gone {
			delete(s.byEntity, entity)
		}
	}
}

func (s *Service) build(r protocol.InspectResult) Inspected {
	c := enrich(r, s.store)
	rep, graded := grading.Grade(c, s.rules)
	icons := map[gear.Slot]string{}
	for slot, it := range c.Items {
		if fileExists(s.store.IconPath(it.ID)) {
			icons[slot] = "/icons/" + strconv.Itoa(it.ID) + ".png"
		}
	}
	// With no item data every slot scores 0, which the UI would show as a D at 0%
	// instead of "not enough data yet", so that is not a grade worth presenting.
	return Inspected{Character: c, Report: rep, Graded: graded && anyKnown(c), Icons: icons}
}

// claimUnknown returns unknown ids this caller must fetch (marked in flight)
// and the done channels of ids another inspect is already fetching. Ids whose
// fetch failed recently are left alone.
func (s *Service) claimUnknown(c gear.Character) (mine []int, others []chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range c.Items {
		if it.Known {
			continue
		}
		if at, ok := s.failed[it.ID]; ok && time.Since(at) < refetchAfter {
			continue
		}
		if ch, ok := s.inflight[it.ID]; ok {
			others = append(others, ch)
			continue
		}
		s.inflight[it.ID] = make(chan struct{})
		mine = append(mine, it.ID)
	}
	return mine, others
}

func (s *Service) fetchAndUpdate(sessionID int, r protocol.InspectResult, mine []int, others []chan struct{}) {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()
	for _, id := range mine {
		_, err := s.fetcher.Item(ctx, id)
		if err != nil {
			slog.Default().Debug("item fetch failed", "id", id, "error", err)
		}
		s.mu.Lock()
		if err != nil {
			s.failed[id] = time.Now()
		} else {
			delete(s.failed, id)
		}
		close(s.inflight[id])
		delete(s.inflight, id)
		s.mu.Unlock()
	}
	for _, ch := range others {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}
	in := s.build(r)
	in.ID = sessionID
	if !anyKnown(in.Character) {
		return
	}
	s.mu.Lock()
	i := s.indexOfLocked(sessionID)
	if i >= 0 {
		s.session[i] = in
	}
	s.mu.Unlock()
	if i >= 0 {
		s.emit("inspect:update", in)
	}
}

func anyKnown(c gear.Character) bool {
	for _, it := range c.Items {
		if it.Known {
			return true
		}
	}
	return false
}

// IconHandler serves cached icons at /icons/<id>.png and passes everything else on.
func (s *Service) IconHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutPrefix(r.URL.Path, "/icons/")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		idStr, ok := strings.CutSuffix(name, ".png")
		id, err := strconv.Atoi(idStr)
		if !ok || err != nil || id < 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, s.store.IconPath(id))
	})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// API is the Wails-bound surface: the frontend gets these methods and nothing else.
// Wails binds every exported method of a registered service, and OnMessage, SetStatus
// and AddWarning would let the webview forge capture results in an elevated process.
type API struct{ s *Service }

func (s *Service) API() *API { return &API{s} }

func (a *API) Session() []Inspected     { return a.s.Session() }
func (a *API) Status() capture.Status   { return a.s.Status() }
func (a *API) Warnings() []string       { return a.s.Warnings() }
func (a *API) Paths() map[string]string { return a.s.Paths() }
func (a *API) DownloadAssets() error    { return a.s.DownloadAssets() }
