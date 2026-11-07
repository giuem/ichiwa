package app

import (
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
}

func newFeed(fetch func(cursor string) (feedPage, error)) *Feed {
	return &Feed{fetch: fetch, more: true}
}

// reset empties the feed, keeping where it loads from.
func (f *Feed) reset() {
	*f = Feed{fetch: f.fetch, onPage: f.onPage, more: true, gen: f.gen + 1}
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
		f.items = append(f.items, s)
	}
}

// loadMore loads the next page of f, unless one is loading or f ended.
func (app *App) loadMore(f *Feed) {
	if f.loading || !f.more || f.fetch == nil {
		return
	}
	f.loading, f.err = true, ""
	fetch, cursor, gen := f.fetch, f.cursor, f.gen
	async(app, func() (feedPage, error) { return fetch(cursor) }, func(p feedPage, err error) {
		if f.gen != gen {
			return
		}
		f.loading, f.loaded = false, true
		if err != nil {
			f.err = errText(err)
			return
		}
		before := len(f.items)
		f.add(p.items)
		f.cursor, f.more = p.cursor, p.more
		// A page of nothing new ends the feed, so it doesn't loop.
		if len(f.items) == before && cursor != "" {
			f.more = false
		}
		if f.onPage != nil {
			f.onPage(p)
		}
		app.fill(f, f.items[before:])
		app.warm()
	})
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
		app.feeds[channel] = f
	}
	return f
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
