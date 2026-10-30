package gamedata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeNC serves the two NC routes the fetcher uses, shaped like the aion2-api testdata.
type fakeNC struct {
	srv                                    *httptest.Server
	itemHits, listHits, iconHits, evilHits atomic.Int32
	failItems                              bool
	failIDs                                map[int]bool
	redirect                               bool
	bigIcon                                bool
	onItem                                 func(id int)
}

func newFakeNC(t *testing.T, ids ...int) *fakeNC {
	t.Helper()
	f := &fakeNC{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/dict/search/item"):
			f.listHits.Add(1)
			var rows []string
			for _, id := range ids {
				rows = append(rows, fmt.Sprintf(`{"id":%d,"name":"n%d","grade":"Epic","image":"x","categoryName":"c"}`, id, id))
			}
			fmt.Fprintf(w, `{"contents":[%s],"pagination":{"page":1,"size":200,"lastPage":1,"total":%d,"limit":10000}}`,
				strings.Join(rows, ","), len(ids))
		case strings.HasSuffix(r.URL.Path, "/api/gameconst/item"):
			f.itemHits.Add(1)
			if f.failItems {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			id, _ := strconv.Atoi(r.URL.Query().Get("id"))
			if f.onItem != nil {
				f.onItem(id)
			}
			if f.failIDs[id] {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"id":%d,"name":"Sword %d","grade":"Epic","gradeName":"Heroic","icon":"%s/icons/%d.png",
"level":108,"type":"Equip","mainStats":[
{"id":"WeaponFixingDamage","name":"Attack","minValue":"463","value":"627","extra":"0","exceed":false},
{"id":"PvEAmplifyDamage","name":"PvE Damage Boost","value":"5%%","extra":"0%%","exceed":false}],
"subStats":[{"id":"STR","name":"Might","minValue":"81","value":"100"}]}`, id, id, f.srv.URL, id)
		case strings.HasPrefix(r.URL.Path, "/icons/"):
			if r.URL.Path == "/icons/ok.png" {
				f.evilHits.Add(1)
			}
			f.iconHits.Add(1)
			if f.redirect {
				http.Redirect(w, r, "https://evil.example/icons/ok.png", http.StatusFound)
				return
			}
			if f.bigIcon {
				w.Write(make([]byte, maxIconBytes+10))
				return
			}
			w.Write([]byte("PNG"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// rewriteTransport sends every request to the fake server, keeping path and query.
type rewriteTransport struct {
	to   *url.URL
	base http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = rt.to.Scheme, rt.to.Host
	return rt.base.RoundTrip(r)
}

func newTestFetcher(t *testing.T, f *fakeNC) (*Fetcher, *Store) {
	t.Helper()
	u, _ := url.Parse(f.srv.URL)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := iconHosts
	iconHosts = map[string]bool{u.Hostname(): true}
	t.Cleanup(func() { iconHosts = old })
	ft, err := newFetcher(s, &http.Client{Transport: rewriteTransport{u, f.srv.Client().Transport}}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return ft, s
}

func TestFetchItemCaches(t *testing.T) {
	nc := newFakeNC(t)
	ft, s := newTestFetcher(t, nc)
	for range 2 {
		e, err := ft.Item(t.Context(), 11)
		if err != nil {
			t.Fatal(err)
		}
		if e.ID != 11 || e.Name != "Sword 11" || e.Tier != 5 {
			t.Errorf("entry = %+v", e)
		}
		if e.Stats["WeaponFixingDamage"] != 627 || e.Stats["PvEAmplifyDamage"] != 5 || len(e.Stats) != 2 {
			t.Errorf("stats = %v", e.Stats)
		}
	}
	if n := nc.itemHits.Load(); n != 1 {
		t.Errorf("item requests = %d, want 1", n)
	}
	if _, ok := s.Get(11); !ok {
		t.Error("not stored")
	}
	if b, err := os.ReadFile(s.IconPath(11)); err != nil || string(b) != "PNG" {
		t.Errorf("icon = %q, %v", b, err)
	}
}

func TestFetchItemServerError(t *testing.T) {
	nc := newFakeNC(t)
	nc.failItems = true
	ft, s := newTestFetcher(t, nc)
	if _, err := ft.Item(t.Context(), 11); err == nil {
		t.Fatal("want error")
	}
	if _, ok := s.Get(11); ok {
		t.Error("store changed on error")
	}
}

func TestFetchAllProgress(t *testing.T) {
	nc := newFakeNC(t, 1, 2, 3)
	ft, s := newTestFetcher(t, nc)
	ft.batch = 2
	if err := os.MkdirAll(s.IconPath(1)[:len(s.IconPath(1))-len("1.png")], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.IconPath(1), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][2]int
	err := ft.All(t.Context(), func(done, total int) { calls = append(calls, [2]int{done, total}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 || calls[len(calls)-1] != [2]int{3, 3} {
		t.Errorf("progress = %v", calls)
	}
	for _, id := range []int{1, 2, 3} {
		if _, ok := s.Get(id); !ok {
			t.Errorf("missing entry %d", id)
		}
		if _, err := os.Stat(s.IconPath(id)); err != nil {
			t.Errorf("missing icon %d", id)
		}
	}
	if b, _ := os.ReadFile(s.IconPath(1)); string(b) != "OLD" {
		t.Error("existing icon was overwritten")
	}
	if n := nc.iconHits.Load(); n != 2 {
		t.Errorf("icon downloads = %d, want 2", n)
	}
	// a second run fetches nothing new
	before := nc.itemHits.Load()
	if err := ft.All(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if nc.itemHits.Load() != before {
		t.Error("cached items were refetched")
	}
}

func TestFetchAllCancel(t *testing.T) {
	nc := newFakeNC(t, 1, 2, 3, 4)
	ft, s := newTestFetcher(t, nc)
	ft.batch = 2
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := ft.All(ctx, func(done, total int) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, ok := s.Get(1); !ok {
		t.Error("first batch not persisted")
	}
	if _, ok := s.Get(4); ok {
		t.Error("fetched past cancellation")
	}
}

func TestFetchAllItemFailure(t *testing.T) {
	nc := newFakeNC(t, 1, 2, 3)
	nc.failIDs = map[int]bool{2: true}
	ft, s := newTestFetcher(t, nc)
	var last [2]int
	err := ft.All(t.Context(), func(done, total int) { last = [2]int{done, total} })
	if err == nil || !strings.Contains(err.Error(), "1 of 3 items failed") {
		t.Fatalf("err = %v", err)
	}
	if last != [2]int{3, 3} {
		t.Errorf("progress = %v", last)
	}
	for id, want := range map[int]bool{1: true, 2: false, 3: true} {
		if _, ok := s.Get(id); ok != want {
			t.Errorf("Get(%d) = %v, want %v", id, ok, want)
		}
	}
}

func TestFetchAllCancelMidBatch(t *testing.T) {
	nc := newFakeNC(t, 1, 2, 3, 4)
	ft, s := newTestFetcher(t, nc)
	ft.batch = 10
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	nc.onItem = func(id int) {
		if id == 3 {
			cancel()
		}
	}
	err := ft.All(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	for _, id := range []int{1, 2} {
		if _, ok := s.Get(id); !ok {
			t.Errorf("entry %d lost on cancel", id)
		}
	}
	s2, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get(2); !ok {
		t.Error("entry 2 not on disk")
	}
}

func TestFetchIconRejected(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		nc := newFakeNC(t)
		nc.bigIcon = true
		ft, s := newTestFetcher(t, nc)
		if _, err := ft.Item(t.Context(), 5); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(s.IconPath(5)); !os.IsNotExist(err) {
			t.Errorf("oversized icon saved: %v", err)
		}
		if m, _ := filepath.Glob(filepath.Join(filepath.Dir(s.IconPath(5)), "*.tmp")); len(m) != 0 {
			t.Errorf("temp files left: %v", m)
		}
	})
	t.Run("redirect to disallowed host", func(t *testing.T) {
		nc := newFakeNC(t)
		nc.redirect = true
		ft, s := newTestFetcher(t, nc)
		if _, err := ft.Item(t.Context(), 5); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(s.IconPath(5)); !os.IsNotExist(err) {
			t.Errorf("redirected icon saved: %v", err)
		}
		if n := nc.evilHits.Load(); n != 0 {
			t.Errorf("redirect target requested %d times", n)
		}
	})
	t.Run("host not allowed", func(t *testing.T) {
		nc := newFakeNC(t)
		ft, s := newTestFetcher(t, nc)
		iconHosts = map[string]bool{"assets.playnccdn.com": true}
		if _, err := ft.Item(t.Context(), 5); err != nil {
			t.Fatal(err)
		}
		if n := nc.iconHits.Load(); n != 0 {
			t.Errorf("icon fetched from disallowed host (%d hits)", n)
		}
		if _, err := os.Stat(s.IconPath(5)); !os.IsNotExist(err) {
			t.Error("icon saved")
		}
	})
}
