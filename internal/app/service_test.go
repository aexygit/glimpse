package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/gear"
	"github.com/aexygit/glimpse/internal/grading"
	"github.com/aexygit/glimpse/internal/protocol"
)

type event struct {
	name string
	data any
}

type recorder struct {
	ch chan event
}

func newRecorder() *recorder { return &recorder{ch: make(chan event, 16)} }

func (r *recorder) emit(name string, data any) { r.ch <- event{name, data} }

func (r *recorder) next(t *testing.T) event {
	t.Helper()
	select {
	case e := <-r.ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
		return event{}
	}
}

type toServer struct {
	to   *url.URL
	base http.RoundTripper
}

func (t toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = t.to.Scheme, t.to.Host
	return t.base.RoundTrip(r)
}

func newSvc(t *testing.T, hc *http.Client, rules map[gear.Class]grading.Rules) (*Service, *recorder) {
	t.Helper()
	store := openStore(t)
	f, err := gamedata.NewFetcher(store, hc)
	if err != nil {
		t.Fatal(err)
	}
	rec := newRecorder()
	return New(context.Background(), store, f, rules, nil, rec.emit), rec
}

func offline() *http.Client {
	return &http.Client{Transport: roundFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("offline")
	})}
}

type roundFunc func(*http.Request) (*http.Response, error)

func (f roundFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestServiceInspectUnknownClass(t *testing.T) {
	s, rec := newSvc(t, offline(), map[gear.Class]grading.Rules{})
	s.OnMessage(protocol.InspectResult{Name: "x", Class: "nope", Items: []protocol.InspectItem{{Slot: "Ring1", ID: 9}}})
	e := rec.next(t)
	in, ok := e.data.(Inspected)
	if e.name != "inspect" || !ok || in.Graded || in.ID != 1 {
		t.Fatalf("event = %+v", e)
	}
	if got := s.Session(); len(got) != 1 {
		t.Errorf("session = %d", len(got))
	}
}

func TestServiceFetchesUnknown(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":7,"name":"Blade","grade":"Epic","gradeName":"Heroic","icon":"","level":1,"type":"Equip",
"mainStats":[{"id":"WeaponFixingDamage","name":"Attack","value":"100"}],"subStats":[]}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	hc := &http.Client{Transport: toServer{u, srv.Client().Transport}}
	s, rec := newSvc(t, hc, map[gear.Class]grading.Rules{})
	s.OnMessage(protocol.InspectResult{Class: "Assassin", Items: []protocol.InspectItem{{Slot: "MainHand", ID: 7}}})
	if e := rec.next(t); e.name != "inspect" || e.data.(Inspected).Character.Items["MainHand"].Known {
		t.Fatalf("first event = %+v", e)
	}
	e := rec.next(t)
	it := e.data.(Inspected).Character.Items["MainHand"]
	if e.name != "inspect:update" || !it.Known || it.Name != "Blade" || it.Stats["WeaponFixingDamage"] != 100 {
		t.Fatalf("update = %+v", e)
	}
	if got := s.Session()[0].Character.Items["MainHand"]; !got.Known {
		t.Errorf("session not updated: %+v", got)
	}
}

func TestSessionCap(t *testing.T) {
	s, rec := newSvc(t, offline(), nil)
	for i := range maxSession + 5 {
		s.OnMessage(protocol.InspectResult{Entity: i + 1}) // one entity per character
		rec.next(t)
	}
	got := s.Session()
	if len(got) != maxSession || got[0].ID != 6 || got[len(got)-1].ID != maxSession+5 {
		t.Errorf("len=%d first=%d", len(got), got[0].ID)
	}
}

func TestIconHandler(t *testing.T) {
	s, _ := newSvc(t, offline(), nil)
	p := s.store.IconPath(5)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := s.IconHandler(next)
	for path, want := range map[string]int{
		"/icons/5.png":      200,
		"/icons/6.png":      404,
		"/icons/..%2Fx.png": 404,
		"/icons/../5.png":   404,
		"/icons/a.png":      404,
		"/icons/-1.png":     404,
		"/other":            204,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s = %d, want %d", path, w.Code, want)
		}
		if want == 200 && w.Header().Get("Content-Type") != "image/png" {
			t.Errorf("content type %q", w.Header().Get("Content-Type"))
		}
	}
}

func TestSetupLogRotates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "glimpse.log"), make([]byte, 6<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	_, c, err := SetupLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := os.Stat(filepath.Join(dir, "glimpse.old.log")); err != nil {
		t.Error(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "glimpse.log")); err != nil || fi.Size() != 0 {
		t.Errorf("new log: %v %v", fi, err)
	}
}

func okServer(t *testing.T, gate chan struct{}) *http.Client {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate != nil {
			<-gate
		}
		fmt.Fprint(w, `{"id":7,"name":"Blade","grade":"Epic","gradeName":"Heroic","icon":"","level":1,"type":"Equip",
"mainStats":[{"id":"WeaponFixingDamage","name":"Attack","value":"100"}],"subStats":[]}`)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &http.Client{Transport: toServer{u, srv.Client().Transport}}
}

func TestServiceSharedInflightBothUpdated(t *testing.T) {
	gate := make(chan struct{})
	s, rec := newSvc(t, okServer(t, gate), map[gear.Class]grading.Rules{})
	items := []protocol.InspectItem{{Slot: "MainHand", ID: 7}}
	// Two characters wearing the same item: one fetch, both entries updated.
	s.OnMessage(protocol.InspectResult{Entity: 1, Items: items})
	s.OnMessage(protocol.InspectResult{Entity: 2, Items: items})
	close(gate)
	updated := map[int]bool{}
	for range 4 {
		e := rec.next(t)
		if e.name == "inspect:update" {
			in := e.data.(Inspected)
			if !in.Character.Items["MainHand"].Known {
				t.Errorf("update %d not known", in.ID)
			}
			updated[in.ID] = true
		}
	}
	if !updated[1] || !updated[2] {
		t.Errorf("updated = %v", updated)
	}
}

func TestServiceNoUpdateWhenStillUnknown(t *testing.T) {
	s, rec := newSvc(t, offline(), map[gear.Class]grading.Rules{})
	s.OnMessage(protocol.InspectResult{Items: []protocol.InspectItem{{Slot: "Ring1", ID: 9}}})
	rec.next(t)
	select {
	case e := <-rec.ch:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWarnings(t *testing.T) {
	s, rec := newSvc(t, offline(), nil)
	s.AddWarning("bad rules")
	if e := rec.next(t); e.name != "rules:warning" {
		t.Errorf("event %+v", e)
	}
	if w := s.Warnings(); len(w) != 1 || w[0] != "bad rules" {
		t.Errorf("warnings = %v", w)
	}
}

func TestServiceEmptyStateIsNotNil(t *testing.T) {
	s, _ := newSvc(t, offline(), nil)
	a := s.API()
	// A nil slice marshals to JSON null, which the webview then iterates and throws on.
	if s.Session() == nil || s.Warnings() == nil || a.Session() == nil || a.Warnings() == nil {
		t.Error("empty session or warnings is nil")
	}
}

func TestServiceRepeatEntityUpdatesInPlace(t *testing.T) {
	s, rec := newSvc(t, offline(), nil)
	s.OnMessage(protocol.InspectResult{Entity: 42, Name: "Rin", Level: 36})
	if e := rec.next(t); e.name != "inspect" || e.data.(Inspected).ID != 1 {
		t.Fatalf("first event = %+v", e)
	}
	s.OnMessage(protocol.InspectResult{Entity: 42, Name: "Rin", Level: 40})
	e := rec.next(t)
	in, ok := e.data.(Inspected)
	if e.name != "inspect:update" || !ok || in.ID != 1 || in.Character.Level != 40 {
		t.Fatalf("repeat event = %+v", e)
	}
	got := s.Session()
	if len(got) != 1 || got[0].ID != 1 || got[0].Character.Level != 40 {
		t.Errorf("session = %+v, want one entry updated in place", got)
	}
}

func TestServiceNotGradedWithoutItemData(t *testing.T) {
	rules := map[gear.Class]grading.Rules{"assassin": {
		Class:      "assassin",
		Weights:    map[gear.Stat]float64{"attack": 1},
		BIS:        map[gear.Slot]grading.BIS{"MainHand": {Tier: 5, Enchant: 15, Stats: gear.Stats{"attack": 100}}},
		Thresholds: map[string]float64{"S": 0.95},
	}}
	s, rec := newSvc(t, offline(), rules)
	s.OnMessage(protocol.InspectResult{Entity: 1, Class: "assassin",
		Items: []protocol.InspectItem{{Slot: "MainHand", ID: 9}}})
	if in := rec.next(t).data.(Inspected); in.Graded {
		t.Errorf("graded with no cached item: %+v", in.Report)
	}
	if err := s.store.Put(gamedata.Entry{ID: 10, Name: "Blade", Tier: 5, Stats: gear.Stats{"attack": 100}}); err != nil {
		t.Fatal(err)
	}
	s.OnMessage(protocol.InspectResult{Entity: 2, Class: "assassin",
		Items: []protocol.InspectItem{{Slot: "MainHand", ID: 10, Enchant: 15}}})
	if in := rec.next(t).data.(Inspected); !in.Graded {
		t.Errorf("not graded with a cached item: %+v", in)
	}
}

func TestClaimUnknownSkipsRecentFailure(t *testing.T) {
	s, _ := newSvc(t, offline(), nil)
	c := gear.Character{Items: map[gear.Slot]gear.Item{"Ring1": {ID: 9}}}
	if mine, others := s.claimUnknown(c); len(mine) != 1 || len(others) != 0 {
		t.Fatalf("first claim = %v %v, want the id", mine, others)
	}
	s.mu.Lock()
	delete(s.inflight, 9)
	s.failed[9] = time.Now()
	s.mu.Unlock()
	if mine, others := s.claimUnknown(c); len(mine) != 0 || len(others) != 0 {
		t.Errorf("claim after a failure = %v %v, want nothing", mine, others)
	}
	s.mu.Lock()
	s.failed[9] = time.Now().Add(-2 * refetchAfter)
	s.mu.Unlock()
	if mine, _ := s.claimUnknown(c); len(mine) != 1 {
		t.Errorf("claim past the retry window = %v, want the id again", mine)
	}
}

func TestServiceRecordsFetchFailure(t *testing.T) {
	s, rec := newSvc(t, offline(), nil)
	s.OnMessage(protocol.InspectResult{Entity: 1, Items: []protocol.InspectItem{{Slot: "Ring1", ID: 9}}})
	rec.next(t)
	for deadline := time.Now().Add(5 * time.Second); ; {
		s.mu.Lock()
		_, ok := s.failed[9]
		s.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a failed fetch was not remembered, so it will be retried forever")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDownloadAssetsOneAtATime(t *testing.T) {
	gate, hit := make(chan struct{}), make(chan struct{}, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		<-gate
		http.Error(w, "stop", http.StatusInternalServerError)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	s, _ := newSvc(t, &http.Client{Transport: toServer{u, srv.Client().Transport}}, nil)
	done := make(chan error, 1)
	go func() { done <- s.DownloadAssets() }()
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("the first download never reached the catalog")
	}
	// In a goroutine: without the guard the second call blocks on the gated
	// server instead of returning, and the test would hang rather than fail.
	second := make(chan error, 1)
	go func() { second <- s.DownloadAssets() }()
	select {
	case err := <-second:
		if err == nil || !strings.Contains(err.Error(), "already running") {
			t.Errorf("second download = %v, want it refused", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("the second download was not refused, it started a run of its own")
	}
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the first download did not finish")
	}
	if s.downloading.Load() {
		t.Error("the guard was not released after the run")
	}
}
