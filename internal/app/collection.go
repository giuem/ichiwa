package app

import (
	"maps"
	"slices"

	"github.com/giuem/ichiwa/internal/source"
)

// Collection is a collection page: its filters and its pages.
type Collection struct {
	ID string
	// title is the navigation item's or the card's, until the page says.
	title string
	// card is the card the page opened from, whose image stands in for
	// the header's until it comes.
	Card   *Card
	Rows   []FilterRow
	Chosen map[string]string
	First  *source.CollectionPage // the first page: title, header, sections, layout
	// haveFirst tells first is the first page of the current choice, one
	// whose list did not fail: a retry's first page replaces one that did.
	haveFirst bool
	// header is first's, or the one the cache kept, shown until it comes.
	Header *source.Header
	// savedHeader is the header saved last of a page whose items are not.
	savedHeader *source.Header
	Results     Feed
	sections    map[string]*Feed // sections the host loads, by collection ID
}

func newCol(id, title string) *Collection {
	cs := &Collection{ID: id, title: title, sections: map[string]*Feed{}}
	cs.Results.More = true
	return cs
}

// col is the collection page of a navigation item of the source shown.
func (app *App) Col(id string) *Collection {
	if app.cols == nil {
		app.cols = map[string]*Collection{}
	}
	cs := app.cols[id]
	if cs == nil {
		cs = newCol(id, app.NavTitle(id))
		cs.Results.Src = app.Src
		if app.CacheDir != "" && app.Src != nil {
			cs.Results.cache = feedCachePath(app.CacheDir, app.SourceID(), id)
			if c := loadFeedCache(cs.Results.cache); c == nil {
				if c = app.legacyFeedCache(id, cs.Results.cache); c != nil {
					cs.Results.Items, cs.Header = c.Items, c.Header
				}
			} else {
				cs.Results.Items, cs.Header = c.Items, c.Header
			}
			cs.Results.stale = len(cs.Results.Items) > 0
		}
		app.cols[id] = cs
	}
	return cs
}

// ranked tells the page ranks its cards.
func (cs *Collection) Ranked() bool { return cs.First != nil && cs.First.Layout == source.LayoutRanked }

func (cs *Collection) applyPage(fp feedPage) {
	if fp.Page == nil || cs.haveFirst {
		return
	}
	cs.haveFirst = !fp.failed
	cs.First = fp.Page
	if fp.Page.Header != nil {
		cs.Header = fp.Page.Header // else the header shown stays, as a retry's page may lack it
	}
	cs.Rows = fp.Page.Filters
	next := map[string]string{}
	for _, row := range cs.Rows {
		if row.Value != "" {
			next[row.Key] = row.Value
		}
	}
	cs.Chosen = next
}

// choose picks value in the row key, "" for all, and loads the page for
// the new choice. A ranking drops the rows after the one changed, whose
// options belong to the choice before, until the page brings its own.
func (cs *Collection) Choose(key, value string) bool {
	if cs.Chosen[key] == value {
		return false
	}
	if cs.Chosen == nil {
		cs.Chosen = map[string]string{}
	}
	if value == "" {
		delete(cs.Chosen, key)
	} else {
		cs.Chosen[key] = value
	}
	if cs.Ranked() {
		if i := slices.IndexFunc(cs.Rows, func(r FilterRow) bool { return r.Key == key }); i >= 0 {
			cs.Rows = cs.Rows[:i+1]
		}
	}
	cs.restart()
	return true
}

// clear goes back to the page without filters.
func (cs *Collection) Clear() {
	cs.Chosen = nil
	cs.restart()
}

// restart drops the results, and loads in flight, for a new choice. The
// view gives the feed a fetch for it.
func (cs *Collection) restart() {
	cs.haveFirst = false
	cs.Results.reset()
	cs.Results.fetch = nil
}

// refresh loads the page anew, its sections too.
func (cs *Collection) Refresh() {
	cs.restart()
	for _, f := range cs.sections {
		f.reset()
	}
	cs.sections = map[string]*Feed{}
}

// ensure gives the page's feed its fetch, for the current choice, and
// loads its first page when it has none.
func (app *App) Ensure(cs *Collection) {
	r := &cs.Results
	if r.fetch == nil {
		src := r.Src
		if src == nil {
			src = app.Src
		}
		r.setFetch(src, collectionFetch(source.CollectionRequest{ID: cs.ID, Filters: maps.Clone(cs.Chosen)}))
		r.onPage = cs.applyPage
		r.More = !r.Loaded
	}
	if !r.Loaded && !r.Loading && r.Err == "" {
		app.LoadMore(r) // also a load canceled as the page was left
	}
}

// openCollection opens the collection a card or a section names, over the
// page shown.
func (app *App) OpenCollection(a Card) {
	app.leavePage()
	cs := newCol(a.ID, a.Title)
	card := a
	cs.Card = &card
	cs.Results.Src = app.Src
	app.Stack = append(app.Stack, cs)
}

// closeCollection goes back from the collection opened last.
func (app *App) CloseCollection() {
	app.leavePage()
	if n := len(app.Stack); n > 0 {
		app.Stack[n-1].Results.reset()
		for _, f := range app.Stack[n-1].sections {
			f.reset()
		}
		app.Stack = app.Stack[:n-1]
	}
}

// title is what the page is called.
func (cs *Collection) PageTitle() string {
	switch {
	case cs.Header != nil && cs.Header.Title != "":
		return cs.Header.Title
	case cs.First != nil && cs.First.Title != "":
		return cs.First.Title
	}
	return cs.title
}

// sectionFeed is the feed of a section that names its collection rather
// than give its cards. A section is one page: loaded when shown, again
// when a load was canceled as the page was left.
func (app *App) SectionFeed(feeds map[string]*Feed, src source.Source, id string) *Feed {
	f := feeds[id]
	if f == nil {
		f = newFeed(src, collectionFetch(source.CollectionRequest{ID: id}))
		feeds[id] = f
	}
	if !f.Loaded && !f.Loading && f.Err == "" {
		f.More = true
		app.LoadMore(f)
	}
	f.More = false
	return f
}

// ColSection is the feed of a section of cs that names its collection.
func (app *App) ColSection(cs *Collection, id string) *Feed {
	return app.SectionFeed(cs.sections, cs.Results.Src, id)
}

// OpenCollectionOf opens a collection of the source sourceID over its
// pages, leaving the player page: from the details of a drama of another
// source, that source shows.
func (app *App) OpenCollectionOf(sourceID string, a Card) {
	if sourceID != "" && sourceID != app.SourceID() {
		if !app.Available(sourceID) {
			app.Unavailable = &Unavailable{Source: sourceID, Name: app.SourceName(sourceID), Title: a.Title,
				Installed: app.Sources.Get(sourceID) != nil}
			return
		}
		app.SwitchSource(app.Sources.Get(sourceID))
	}
	app.CloseWatch()
	if appPage(app.Page) {
		app.Go(startPage(app.Nav, app.srcPage, app.HasCap(source.CapSearch)))
	}
	app.OpenCollection(a)
}
