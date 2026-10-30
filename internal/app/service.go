// Package app wires capture, game data and grading for the frontend.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aexygit/glimpse/internal/capture"
	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/gear"
	"github.com/aexygit/glimpse/internal/grading"
	"github.com/aexygit/glimpse/internal/protocol"
)

// ponytail: session keeps the last 200 inspections in memory; persist to disk if history matters.
const maxSession = 200

type Inspected struct {
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

	mu       sync.Mutex
	session  []Inspected
	nextID   int
	inflight map[int]chan struct{}
	warnings []string
	status   capture.Status
}

// New builds a Service. ctx bounds background fetches; emit may be called from any goroutine.
func New(ctx context.Context, store *gamedata.Store, fetcher *gamedata.Fetcher, rules map[gear.Class]grading.Rules,
	paths map[string]string, emit func(string, any)) *Service {
	return &Service{ctx: ctx, store: store, fetcher: fetcher, rules: rules, paths: paths, emit: emit,
		inflight: map[int]chan struct{}{}}
}

func (s *Service) Session() []Inspected {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Inspected(nil), s.session...)
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
	return append([]string(nil), s.warnings...)
}

func (s *Service) Paths() map[string]string { return s.paths }

// SetStatus records and emits the capture status.
func (s *Service) SetStatus(st capture.Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	s.emit("status", st)
}

func (s *Service) DownloadAssets() error {
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
	s.mu.Lock()
	s.nextID++
	in.ID = s.nextID
	s.session = append(s.session, in)
	if len(s.session) > maxSession {
		s.session = s.session[1:]
	}
	s.mu.Unlock()
	s.emit("inspect", in)

	if mine, others := s.claimUnknown(in.Character); len(mine)+len(others) > 0 {
		go s.fetchAndUpdate(in.ID, r, mine, others)
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
	return Inspected{Character: c, Report: rep, Graded: graded, Icons: icons}
}

// claimUnknown returns unknown ids this caller must fetch (marked in flight)
// and the done channels of ids another inspect is already fetching.
func (s *Service) claimUnknown(c gear.Character) (mine []int, others []chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range c.Items {
		if it.Known {
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
		if _, err := s.fetcher.Item(ctx, id); err != nil {
			slog.Default().Debug("item fetch failed", "id", id, "error", err)
		}
		s.mu.Lock()
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
	found := false
	for i := range s.session {
		if s.session[i].ID == sessionID {
			s.session[i], found = in, true
		}
	}
	s.mu.Unlock()
	if found {
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
