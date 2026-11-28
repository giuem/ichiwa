package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/sources/hongguo"
)

// feedPage is one page of a feed as its source returns it.
type feedPage struct {
	Items  []Card
	cursor string
	More   bool
	Page   *source.CollectionPage // full protocol page for onPage (filters, header, sections)
	// failed tells the page came with an error: its list failed, and a
	// retry asks for it again.
	failed bool
}

func pageResult(p *source.CollectionPage, err error) (feedPage, error) {
	if p == nil {
		return feedPage{}, err
	}
	return feedPage{Items: p.Items, cursor: p.Cursor, More: p.Cursor != "", Page: p}, err
}

// Feed is a list of cards loaded page by page from fetch. The view keeps
// about two screens loaded below what it shows, so pages arrive before
// they are scrolled to, and a page that doesn't fill the view loads the
// next at once.
type Feed struct {
	// src is the source the feed loads from, and fetch loads a page of
	// it, off the main thread; set together by setFetch, so that a feed
	// keeps to its source when the app switches to another.
	Src    source.Source
	fetch  fetchFunc
	onPage func(feedPage) // runs on the main thread

	Items   []Card
	seen    map[string]bool
	cursor  string
	More    bool
	Loading bool
	Loaded  bool
	Err     string
	// UI is the view's own state of the list, which the app keeps
	// with it and never reads.
	UI     any
	gen    int // bumped by reset, so results of earlier loads are dropped
	stalls int // pages in a row that brought nothing new
	// retryAt is when a source that limited the rate lets the feed ask
	// again (protocol §10); the error stays until then.
	RetryAt time.Time
	waitErr string

	// cache is the file that keeps the first items of the feed for the
	// next launch, empty for a feed not kept. stale tells the items came
	// from it, and changed that the items it keeps changed since saved.
	cache   string
	stale   bool
	changed bool

	// life ends when the feed resets, canceling the calls for what it
	// showed; end ends it.
	life context.Context
	end  context.CancelFunc
}

// fetchFunc loads the page of a feed at cursor from src.
type fetchFunc func(ctx context.Context, src source.Source, cursor string) (feedPage, error)

func newFeed(src source.Source, fetch fetchFunc) *Feed {
	return &Feed{Src: src, fetch: fetch, More: true}
}

// setFetch sets where f loads from.
func (f *Feed) setFetch(src source.Source, fetch fetchFunc) { f.Src, f.fetch = src, fetch }

// collectionFetch loads the pages of the collection req names.
func collectionFetch(req source.CollectionRequest) fetchFunc {
	return func(ctx context.Context, src source.Source, cursor string) (feedPage, error) {
		r := req
		r.Cursor = cursor
		return pageResult(src.Collection(ctx, r))
	}
}

// reset empties the feed, keeping where it loads from, and cancels its
// calls.
func (f *Feed) reset() {
	if f.end != nil {
		f.end()
	}
	*f = Feed{Src: f.Src, fetch: f.fetch, onPage: f.onPage, More: true, gen: f.gen + 1, cache: f.cache,
		RetryAt: f.RetryAt, waitErr: f.waitErr}
}

// context is ctx, canceled also when f resets; done releases it.
func (f *Feed) context(ctx context.Context) (_ context.Context, done func()) {
	if f.life == nil {
		f.life, f.end = context.WithCancel(context.Background())
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(f.life, cancel)
	return ctx, func() { stop(); cancel() }
}

func (f *Feed) add(items []Card) {
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	for _, s := range items {
		if s.ID == "" || f.seen[s.ID] {
			continue
		}
		f.seen[s.ID] = true
		if len(f.Items) < feedCacheSize {
			f.changed = true
		}
		f.Items = append(f.Items, s)
	}
}

// take adds a page to f and returns whether it brought anything new and
// the items to fill. The first page after items of the cache goes before
// them, and they stay after it, so the grid doesn't shrink as it comes.
func (f *Feed) take(p feedPage) (fresh bool, added []Card) {
	if f.stale {
		cached := f.Items
		f.Items, f.seen, f.stale = nil, nil, false
		f.add(p.Items)
		fresh = len(f.Items) > 0
		f.add(cached)
		return fresh, f.Items
	}
	before := len(f.Items)
	f.add(p.Items)
	return len(f.Items) > before, f.Items[before:]
}

// loadMore loads the next page of f, unless one is loading or f ended,
// for the page shown: leaving it cancels the load.
func (app *App) LoadMore(f *Feed) { app.loadMoreIn(app.pageContext(), f) }

// loadMoreIn loads the next page of f until ctx ends. A load canceled
// leaves f as it was before, to load again when shown.
func (app *App) loadMoreIn(ctx context.Context, f *Feed) { app.load(ctx, f, nil) }

// load loads the next page of f until ctx ends, and runs then, if not
// nil, when it is done. It tells whether it loads: then does not run
// when it doesn't.
func (app *App) load(ctx context.Context, f *Feed, then func()) bool {
	if f.Loading || !f.More || f.fetch == nil || f.Src == nil {
		return false
	}
	if f.RetryWait() > 0 {
		f.Err = f.waitErr // a retry clears it: it stays until the wait ends
		return false
	}
	f.Loading, f.Err = true, ""
	src, fetch, cursor, gen := f.Src, f.fetch, f.cursor, f.gen
	wait := time.Duration(f.stalls) * stallWait
	ctx, done := f.context(ctx)
	async(app, func() (feedPage, error) {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return feedPage{}, ctx.Err()
		}
		return fetch(ctx, src, cursor)
	}, func(p feedPage, err error) {
		defer done()
		if then != nil {
			defer then()
		}
		if f.gen != gen {
			return
		}
		f.Loading = false
		if ctx.Err() != nil {
			return // canceled by the app: not an error, and dropped
		}
		if err != nil {
			f.Loaded = true
			if p.Page != nil && f.onPage != nil {
				p.failed = true
				f.onPage(p)
			}
			f.Err = errText(err)
			if wait := source.RetryAfter(err); wait > 0 {
				f.RetryAt, f.waitErr = time.Now().Add(wait), f.Err
				app.countdown(f.RetryAt)
			}
			return
		}
		f.Loaded = true
		fresh, added := f.take(p)
		f.More = f.Step(p, fresh)
		if f.onPage != nil {
			f.onPage(p)
		}
		// Covers come for the source rather than the page, so that
		// cards shown again have them.
		fillCtx := app.srcContext()
		if source.IsBackground(ctx) {
			fillCtx = source.Background(fillCtx)
		}
		app.fill(fillCtx, f, added)
		app.warm()
	})
	return true
}

// retryWait is how many Seconds the feed still waits to ask again.
func (f *Feed) RetryWait() int {
	if d := time.Until(f.RetryAt); d > 0 {
		return int(math.Ceil(d.Seconds()))
	}
	return 0
}

// errText is the feed's error as shown, with the wait before a retry.
func (f *Feed) ErrText() string {
	if f.Err == "" {
		return ""
	}
	if s := f.RetryWait(); s > 0 {
		return fmt.Sprintf("%s（%d 秒后可以重试）", f.Err, s)
	}
	return f.Err
}

// countdown redraws the window each second until, for a countdown.
func (app *App) countdown(until time.Time) {
	if app.host == nil {
		return
	}
	go func() {
		for time.Now().Before(until) {
			time.Sleep(time.Second)
			app.redraw()
		}
	}()
}

// A list ends where its source gives no cursor (protocol §6). A source
// may also ask for the same list again, as Hongguo does where its server
// says a list ended while asking again goes on; so a feed ends, too,
// after maxStalls pages in a row bring nothing new, asking each time a
// little later.
const (
	maxStalls = 3
	stallWait = 700 * time.Millisecond
)

// step takes in where page p leaves f, which it added to if fresh, and
// reports whether more may come.
func (f *Feed) Step(p feedPage, fresh bool) bool {
	if !p.More || p.cursor == "" {
		return false
	}
	f.cursor = p.cursor // a page of repeats still moves on
	switch {
	case fresh:
		f.stalls = 0
		return true
	case len(f.Items) == 0:
		return false // nothing to show: an empty search, say, ends at once
	}
	f.stalls++
	return f.stalls < maxStalls
}

func cardLacking(c Card) bool {
	return c.Kind == source.CardMedia && (c.Image == "" || len(c.Badges) == 0)
}

// fill looks up the covers and badges that new media cards lack via Refresh,
// and merges them in when they arrive.
func (app *App) fill(ctx context.Context, f *Feed, fresh []Card) {
	src := f.Src
	if src == nil || !src.Info().Has(source.CapRefresh) {
		return
	}
	var ids []string
	for _, s := range fresh {
		if cardLacking(s) {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	gen := f.gen
	ctx, done := f.context(ctx)
	async(app, func() ([]Card, error) {
		var filled []Card
		for start := 0; start < len(ids) && ctx.Err() == nil; start += source.MaxRefreshIDs {
			batch := ids[start:min(start+source.MaxRefreshIDs, len(ids))]
			got, err := src.Refresh(ctx, batch)
			if err != nil {
				continue
			}
			filled = append(filled, got...)
		}
		return filled, nil
	}, func(filled []Card, _ error) {
		defer done()
		if f.gen != gen || len(filled) == 0 || ctx.Err() != nil {
			return
		}
		byID := make(map[string]Card, len(filled))
		for _, s := range filled {
			byID[s.ID] = s
		}
		for i, s := range f.Items {
			if d, ok := byID[s.ID]; ok {
				if s.Image == "" {
					s.Image = d.Image
				}
				if len(s.Badges) == 0 {
					s.Badges = d.Badges
				}
				if s.Subtitle == "" {
					s.Subtitle = d.Subtitle
				}
				f.Items[i] = s
				f.changed = f.changed || i < feedCacheSize
			}
		}
	})
}

// Visible is what of f the grid shows: whole rows of n while more pages
// come, so that the last row doesn't fill in piece by piece.
func Visible(f *Feed, n int) []Card {
	if f.More && f.Err == "" && len(f.Items) > n {
		return f.Items[:len(f.Items)/n*n]
	}
	return f.Items
}

// feedCache is what a feed keeps for the next launch: the header of its
// page and its first items.
type feedCache struct {
	Header *source.Header `json:"header,omitempty"`
	Items  []Card         `json:"items"`
}

// feedCachePath is the file that keeps the first items of a collection of
// a source, named by neither ID as given (protocol §3).
func feedCachePath(cacheDir, sourceID, colID string) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + colID))
	return filepath.Join(cacheDir, "feeds", hex.EncodeToString(sum[:16])+".json")
}

// feedCacheSize is how many items of a collection are kept: a few screens.
const feedCacheSize = 48

func loadFeedCache(path string) *feedCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c feedCache
	if err := json.Unmarshal(data, &c); err != nil {
		log.Printf("feeds: %s is unreadable: %v", path, err)
		return nil
	}
	return &c
}

// legacyFeedNames are the files that kept Hongguo's channels before the
// collections were named by hash: by source, and before that for the
// only source.
var legacyFeedNames = map[string]string{"channel:推荐": "recommend", "channel:真人剧": "live", "channel:漫剧": "comic"}

// legacyFeedCache reads what an earlier version kept of a Hongguo channel
// and moves it to path.
func (app *App) legacyFeedCache(colID, path string) *feedCache {
	name := legacyFeedNames[colID]
	if name == "" || app.SourceID() != hongguo.SourceID {
		return nil
	}
	var items []Card
	bySource := filepath.Join(app.CacheDir, "feeds", hongguo.SourceID, name+".json")
	oldest := filepath.Join(app.CacheDir, "feeds", name+".json")
	if data, err := os.ReadFile(bySource); err == nil {
		if json.Unmarshal(data, &items) != nil {
			items = nil
		}
		defer os.Remove(bySource)
	}
	if len(items) == 0 {
		items = loadLegacyFeedCache(oldest)
	}
	if len(items) == 0 {
		return nil
	}
	c := &feedCache{Items: items}
	// The old files go only once the new one holds their items.
	if err := writeFeedCache(path, c); err == nil {
		_ = os.Remove(oldest)
	}
	return c
}

type legacyFeedShow struct {
	ID       string
	Title    string
	Desc     string
	Cover    string
	Episodes int
	Tag      string
}

func loadLegacyFeedCache(path string) []Card {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var old []legacyFeedShow
	if err := json.Unmarshal(data, &old); err != nil {
		log.Printf("feeds: legacy %s is unreadable: %v", path, err)
		_ = os.Remove(path) // it will never read; the feed fills it anew
		return nil
	}
	out := make([]Card, 0, len(old))
	for _, s := range old {
		if s.ID == "" {
			continue
		}
		var badges []string
		if s.Episodes > 0 {
			badges = []string{fmt.Sprintf("全%d集", s.Episodes)}
		}
		out = append(out, Card{
			Kind:     source.CardMedia,
			ID:       s.ID,
			Title:    s.Title,
			Subtitle: s.Desc,
			Image:    s.Cover,
			Shape:    source.ShapePortrait,
			Badges:   badges,
			Note:     s.Tag,
		})
	}
	return out
}

// saveFeeds writes the first items of the collections that changed, on a
// goroutine unless wait. Only pages without filters are kept: what a
// filter shows depends on a choice the next launch starts without.
func (app *App) SaveFeeds(wait bool) {
	type job struct {
		path  string
		cache *feedCache
	}
	var jobs []job
	for _, cs := range app.cols {
		f := &cs.Results
		if f.cache == "" || f.stale || !f.changed || len(f.Items) == 0 || cs.First == nil || len(cs.First.Filters) > 0 {
			continue
		}
		f.changed = false
		jobs = append(jobs, job{f.cache, &feedCache{Header: cs.Header, Items: slices.Clone(f.Items[:min(len(f.Items), feedCacheSize)])}})
	}
	if len(jobs) == 0 {
		return
	}
	write := func() {
		for _, j := range jobs {
			if err := writeFeedCache(j.path, j.cache); err != nil {
				log.Printf("feeds: saving %s: %v", j.path, err)
			}
		}
	}
	if wait {
		write()
	} else {
		go write()
	}
}

func writeFeedCache(path string, c *feedCache) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// warmPages is how many pages of a source's navigation warm asks for.
const warmPages = 3

// warm asks once for the first pages of the source's first pages and its
// search home, so they show at once when opened. It asks one at a time,
// ahead of need (source.Background): the calls of the user must not wait
// behind these. A page that fails to warm loads again when shown, with no
// error.
func (app *App) warm() {
	if app.warmed || app.Src == nil {
		return
	}
	app.warmed = true
	var feeds []*Feed
	for _, it := range app.Nav[:min(len(app.Nav), warmPages)] {
		cs := app.Col(it.ID)
		if cs.Results.Loaded || cs.Results.Loading {
			continue
		}
		cs.Results.setFetch(app.Src, collectionFetch(source.CollectionRequest{ID: cs.ID, Filters: maps.Clone(cs.Chosen)}))
		cs.Results.onPage = cs.applyPage
		feeds = append(feeds, &cs.Results)
	}
	// For the source, not the page: they are for the pages to come.
	app.warmNext(source.Background(app.srcContext()), feeds)
}

// warmNext warms feeds one after the other, then the search home.
func (app *App) warmNext(ctx context.Context, feeds []*Feed) {
	for ; len(feeds) > 0; feeds = feeds[1:] {
		if ctx.Err() != nil {
			return // the source was left
		}
		f, gen := feeds[0], feeds[0].gen
		rest := feeds[1:]
		if app.load(ctx, f, func() {
			if f.gen == gen && f.Err != "" {
				f.Err, f.Loaded, f.More = "", false, true
			}
			app.warmNext(ctx, rest)
		}) {
			return
		}
	}
	if ctx.Err() == nil {
		app.loadHot(false)
	}
}
