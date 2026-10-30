package gamedata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	aion2 "github.com/nuriland/aion2-api"

	"github.com/aexygit/glimpse/internal/gear"
)

const (
	maxIconBytes = 2 << 20
	iconTimeout  = 30 * time.Second
	rateLimit    = 5   // requests per second, enforced by the aion2 client
	batchSize    = 200 // entries per Put and per progress callback
)

// iconHosts is the allowlist for icon downloads (https only). Tests override it.
var iconHosts = map[string]bool{"assets.playnccdn.com": true}

// Fetcher reads item data from NC's official TW catalog (English locale).
// Global has no catalog of its own; item IDs are assumed shared with TW.
type Fetcher struct {
	Store *Store

	api   *aion2.Client
	icons *http.Client // no overall Timeout (iconTimeout applies), redirects re-checked
	ua    string
	batch int
}

func userAgent() string {
	return "glimpse/" + Version + " (+https://github.com/aexygit/glimpse)"
}

// NewFetcher uses http.DefaultClient-like defaults when hc is nil.
func NewFetcher(store *Store, hc *http.Client) (*Fetcher, error) {
	return newFetcher(store, hc, rateLimit)
}

func newFetcher(store *Store, hc *http.Client, rps float64) (*Fetcher, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	api, err := aion2.New(aion2.ConfigOpts{
		Region:     aion2.RegionTW,
		Locale:     aion2.LocaleEN,
		UserAgent:  userAgent(),
		RateLimit:  rps,
		HTTPClient: hc,
	})
	if err != nil {
		return nil, fmt.Errorf("create aion2 client: %w", err)
	}
	ic := *hc
	ic.Timeout = 0
	ic.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return checkIconURL(req.URL)
	}
	return &Fetcher{Store: store, api: api, icons: &ic, ua: userAgent(), batch: batchSize}, nil
}

// Item returns the cached entry, or fetches, stores and returns it.
// A failed icon download does not fail the call.
func (f *Fetcher) Item(ctx context.Context, id int) (Entry, error) {
	if e, ok := f.Store.Get(id); ok {
		return e, nil
	}
	e, iconURL, err := f.fetch(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if err := f.Store.Put(e); err != nil {
		return Entry{}, err
	}
	f.icon(ctx, id, iconURL)
	return e, nil
}

// All walks the catalog listing and fetches every item not yet cached.
// progress may be nil. The listing is capped at 10,000 rows by NC.
// A failing item is logged and skipped (retried on next need); the returned
// error then summarizes the failures. Context errors abort after persisting
// what was fetched.
func (f *Fetcher) All(ctx context.Context, progress func(done, total int)) error {
	var ids []int
	for it, err := range f.api.Items(ctx, aion2.ItemSearch{}) {
		if err != nil {
			return fmt.Errorf("list items: %w", err)
		}
		ids = append(ids, it.ID)
	}
	total := len(ids)
	var pending []Entry
	persist := func() error {
		if len(pending) == 0 {
			return nil
		}
		err := f.Store.Put(pending...)
		pending = pending[:0]
		return err
	}
	abort := func(err error) error { return errors.Join(err, persist()) }
	failed := 0
	var lastErr error
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return abort(err)
		}
		if _, ok := f.Store.Get(id); !ok {
			e, iconURL, err := f.fetch(ctx, id)
			switch {
			case ctx.Err() != nil:
				if err == nil {
					pending = append(pending, e)
				}
				return abort(ctx.Err())
			case err != nil:
				slog.Default().Warn("item fetch failed", "id", id, "error", err)
				failed++
				lastErr = err
			default:
				pending = append(pending, e)
				f.icon(ctx, id, iconURL)
			}
		}
		if done := i + 1; len(pending) >= f.batch || done == total || done%f.batch == 0 {
			if err := persist(); err != nil {
				return err
			}
			if progress != nil {
				progress(done, total)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d items failed, last: %w", failed, total, lastErr)
	}
	return nil
}

func (f *Fetcher) fetch(ctx context.Context, id int) (Entry, string, error) {
	it, err := f.api.Item(ctx, id)
	if err != nil {
		return Entry{}, "", fmt.Errorf("fetch item %d: %w", id, err)
	}
	e := Entry{ID: it.ID, Name: it.Name, Tier: tier(it.Grade), Icon: it.ImageURL, Stats: gear.Stats{}}
	for _, s := range it.MainStats {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s.Value, "%"), 64); err == nil {
			e.Stats[gear.Stat(s.ID)] = v
		}
	}
	return e, it.ImageURL, nil
}

// tier maps NC's grade ids (the "grade" field, not the localized gradeName,
// which sits one off in English) by rarity: Common 1 ... Epic 5.
func tier(grade string) int {
	switch grade {
	case "Common":
		return 1
	case "Rare":
		return 2
	case "Legend":
		return 3
	case "Unique":
		return 4
	case "Epic":
		return 5
	}
	return 0
}

// icon downloads to IconPath unless the file exists; failures are logged only.
func (f *Fetcher) icon(ctx context.Context, id int, rawURL string) {
	if rawURL == "" {
		return
	}
	if err := f.downloadIcon(ctx, f.Store.IconPath(id), rawURL); err != nil && ctx.Err() == nil {
		slog.Default().Warn("icon download failed", "id", id, "error", err)
	}
}

func checkIconURL(u *url.URL) error {
	if u.Scheme != "https" || !iconHosts[u.Hostname()] {
		return fmt.Errorf("icon url not allowed: %s://%s", u.Scheme, u.Host)
	}
	return nil
}

func (f *Fetcher) downloadIcon(ctx context.Context, path, rawURL string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if err := checkIconURL(u); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, iconTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", f.ua)
	resp, err := f.icons.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("icon status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "icon-*.tmp")
	if err != nil {
		return err
	}
	n, cerr := io.Copy(tmp, io.LimitReader(resp.Body, maxIconBytes+1))
	if cerr == nil && n > maxIconBytes {
		cerr = errors.New("icon too large")
	}
	if err := errors.Join(cerr, tmp.Close()); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
