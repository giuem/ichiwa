package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hongguo/internal/catalog"
)

// feedPage is one page of a feed as its source returns it.
type feedPage struct {
	items  []Show
	cursor string
	more   bool
	extra  any // what else the source returned, for onPage
}

// Feed is a list of dramas loaded page by page from fetch. The view keeps
// about two screens loaded below what it shows, so pages arrive before
// they are scrolled to, and a page that doesn't fill the view loads the
// next at once.
type Feed struct {
	fetch  func(cursor string) (feedPage, error) // runs off the main thread
	onPage func(feedPage)                        // runs on the main thread

	items   []Show
	seen    map[string]bool
	cursor  string
	more    bool
	loading bool
	loaded  bool
	err     string
	scroll  ui.ScrollState
	gen     int // bumped by reset, so results of earlier loads are dropped
	stalls  int // pages in a row that brought nothing new

	// cache is the file that keeps the first items of the feed for the
	// next launch, empty for a feed not kept. stale tells the items came
	// from it, and changed that the items it keeps changed since saved.
	cache   string
	stale   bool
	changed bool
}

func newFeed(fetch func(cursor string) (feedPage, error)) *Feed {
	return &Feed{fetch: fetch, more: true}
}

// reset empties the feed, keeping where it loads from.
func (f *Feed) reset() {
	*f = Feed{fetch: f.fetch, onPage: f.onPage, more: true, gen: f.gen + 1, cache: f.cache}
}

func (f *Feed) add(items []Show) {
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	for _, s := range items {
		if s.ID == "" || f.seen[s.ID] {
			continue
		}
		f.seen[s.ID] = true
		if len(f.items) < feedCacheSize {
			f.changed = true
		}
		f.items = append(f.items, s)
	}
}

// take adds a page to f and returns whether it brought anything new and
// the items to fill. The first page after items of the cache goes before
// them, and they stay after it, so the grid doesn't shrink as it comes.
func (f *Feed) take(p feedPage) (fresh bool, added []Show) {
	if f.stale {
		cached := f.items
		f.items, f.seen, f.stale = nil, nil, false
		f.add(p.items)
		fresh = len(f.items) > 0
		f.add(cached)
		return fresh, f.items
	}
	before := len(f.items)
	f.add(p.items)
	return len(f.items) > before, f.items[before:]
}

// loadMore loads the next page of f, unless one is loading or f ended.
func (app *App) loadMore(f *Feed) {
	if f.loading || !f.more || f.fetch == nil {
		return
	}
	f.loading, f.err = true, ""
	fetch, cursor, gen := f.fetch, f.cursor, f.gen
	wait := time.Duration(f.stalls) * stallWait
	async(app, func() (feedPage, error) {
		time.Sleep(wait)
		return fetch(cursor)
	}, func(p feedPage, err error) {
		if f.gen != gen {
			return
		}
		f.loading, f.loaded = false, true
		if err != nil {
			f.err = errText(err)
			return
		}
		fresh, added := f.take(p)
		f.more = f.step(p, fresh)
		if f.onPage != nil {
			f.onPage(p)
		}
		app.fill(f, added)
		app.warm()
	})
}

// A source ends a list early now and then: Hongguo's feeds answer a page
// with no way on, or with only repeats, while asking again goes on. So a
// feed takes an end for real only after maxStalls pages in a row bring
// nothing new, asking each time a little later.
const (
	maxStalls = 3
	stallWait = 700 * time.Millisecond
)

// step takes in where page p leaves f, which it added to if fresh, and
// reports whether more may come.
func (f *Feed) step(p feedPage, fresh bool) bool {
	end := !p.more || p.cursor == ""
	if !end {
		f.cursor = p.cursor // a page of repeats still moves on
	}
	switch {
	case fresh && !end:
		f.stalls = 0
		return true
	case len(f.items) == 0:
		return false // nothing to show: an empty search, say, ends at once
	}
	f.stalls++
	return f.stalls < maxStalls
}

// fill looks up the covers and counts that new items of f lack, and puts
// them in when they come.
func (app *App) fill(f *Feed, fresh []Show) {
	var need []Show
	for _, s := range fresh {
		if catalog.Lacking(s) {
			need = append(need, s)
		}
	}
	if len(need) == 0 {
		return
	}
	gen := f.gen
	async(app, func() ([]Show, error) { return app.be.Fill(need), nil }, func(filled []Show, _ error) {
		if f.gen != gen {
			return
		}
		byID := make(map[string]Show, len(filled))
		for _, s := range filled {
			byID[s.ID] = s
		}
		for i, s := range f.items {
			if d, ok := byID[s.ID]; ok {
				f.items[i] = d
				f.changed = f.changed || i < feedCacheSize
			}
		}
	})
}

// pump loads the next page of f, shown in a scroll container of the view
// c builds, while less than about two screens are loaded below the view.
// That covers a feed too short to fill the view, whose MaxY is 0.
func (app *App) pump(c *ui.Context, f *Feed) {
	if !f.more || f.loading || f.err != "" {
		return
	}
	_, h := c.Size()
	if f.scroll.MaxY-f.scroll.Y < 2*h {
		app.loadMore(f)
	}
}

// visible is what of f the grid shows: whole rows of n while more pages
// come, so that the last row doesn't fill in piece by piece.
func visible(f *Feed, n int) []Show {
	if f.more && f.err == "" && len(f.items) > n {
		return f.items[:len(f.items)/n*n]
	}
	return f.items
}

func (app *App) feed(channel string) *Feed {
	f := app.feeds[channel]
	if f == nil {
		f = newFeed(func(cursor string) (feedPage, error) {
			items, next, more, err := app.be.Discover(channel, cursor)
			return feedPage{items: items, cursor: next, more: more}, err
		})
		if name := feedCacheNames[channel]; name != "" && app.cacheDir != "" {
			f.cache = filepath.Join(app.cacheDir, "feeds", name+".json")
			f.items = loadFeedCache(f.cache)
			f.stale = len(f.items) > 0
		}
		app.feeds[channel] = f
	}
	return f
}

// The channels whose first items are kept for the next launch, which
// shows them at once and then what comes, and their files.
var feedCacheNames = map[string]string{pageRecommend: "recommend", pageLive: "live", pageComic: "comic"}

// feedCacheSize is how many items of a channel are kept: a few screens.
const feedCacheSize = 48

func loadFeedCache(path string) []Show {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []Show
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("feeds: %s is unreadable: %v", path, err)
		return nil
	}
	return items
}

// saveFeeds writes the first items of the channels that changed, on a
// goroutine unless wait.
func (app *App) saveFeeds(wait bool) {
	type job struct {
		path  string
		items []Show
	}
	var jobs []job
	for _, f := range app.feeds {
		if f.cache == "" || f.stale || !f.changed || len(f.items) == 0 {
			continue
		}
		f.changed = false
		jobs = append(jobs, job{f.cache, slices.Clone(f.items[:min(len(f.items), feedCacheSize)])})
	}
	if len(jobs) == 0 {
		return
	}
	write := func() {
		for _, j := range jobs {
			if err := writeFeedCache(j.path, j.items); err != nil {
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

func writeFeedCache(path string, items []Show) error {
	data, err := json.Marshal(items)
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

// warm asks once for the first pages of the other pages, so they show at
// once when opened.
func (app *App) warm() {
	if app.warmed || app.be == nil {
		return
	}
	app.warmed = true
	for _, ch := range []string{pageRecommend, pageLive, pageComic} {
		if f := app.feed(ch); !f.loaded {
			app.loadMore(f)
		}
	}
	if app.rank.list.fetch == nil {
		app.loadRank("", "")
	}
	app.loadHot()
}
