package app

import (
	"github.com/giuem/ichiwa/internal/source"
)

// PageSources is the page that manages the sources.
const PageSources = "sources"

// appPage tells page is one of the app's own, not of the source shown.
func appPage(page string) bool {
	return page == PageHistory || page == PageFollows || page == PageSources
}

// browse is what the app showed of a source it left, to show again.
type browse struct {
	src    source.Source
	nav    []source.NavItem
	navErr string
	page   string
	cols   map[string]*Collection
	stack  []*Collection
	search *searchState
	warmed bool
}

// SourceOn tells the source id is turned on.
func (app *App) SourceOn(id string) bool { return !app.Lib.Off[id] }

// SourcesOn is the sources to choose from: those loaded and turned on.
func (app *App) SourcesOn() []source.Source {
	var out []source.Source
	for _, s := range app.Sources.All() {
		if app.SourceOn(s.Info().ID) {
			out = append(out, s)
		}
	}
	return out
}

// firstOn is the first source turned on, nil when there is none.
func (app *App) firstOn() source.Source {
	if on := app.SourcesOn(); len(on) > 0 {
		return on[0]
	}
	return nil
}

// SetSourceOn turns the source id on or off. A source turned off while
// shown gives way to another, and the first turned on when there was
// none shows.
func (app *App) SetSourceOn(id string, on bool) {
	if on == app.SourceOn(id) {
		return
	}
	if on {
		delete(app.Lib.Off, id)
	} else {
		app.Lib.Off[id] = true
		delete(app.kept, id)
	}
	app.Lib.MarkDirty()
	switch {
	case !on && app.Src != nil && app.Src.Info().ID == id:
		app.SwitchSource(app.firstOn())
		delete(app.kept, id)
		app.Page, app.Lib.Page = PageSources, PageSources // the page that turned it off stays
	case on && app.Src == nil:
		page := app.Page
		app.UseSource(app.Sources.Get(id))
		if appPage(page) {
			app.Page, app.Lib.Page = page, page
		}
	}
}

// SwitchSource shows s where the user left it, or at its first page.
// What loads for the source shown before stops.
func (app *App) SwitchSource(s source.Source) {
	if s == app.Src {
		if appPage(app.Page) {
			app.Go(startPage(app.Nav, app.srcPage, app.HasCap(source.CapSearch)))
		}
		return
	}
	app.CloseWatch()
	app.stash()
	app.leaveSource()
	var b *browse
	if s != nil {
		id := s.Info().ID
		if b = app.kept[id]; b != nil && b.src != s {
			b = nil // a plugin that stood in for it, or the other way round
		}
		delete(app.kept, id)
	}
	app.show(s, b)
}

// UseSource shows s afresh, nil for none: its navigation and its first
// page, nothing of what it showed before.
func (app *App) UseSource(s source.Source) {
	app.CloseWatch()
	app.leaveSource()
	if s != nil {
		delete(app.kept, s.Info().ID)
	}
	app.show(s, nil)
}

// stash keeps what the source shown shows, to show again.
func (app *App) stash() {
	if app.Src == nil {
		return
	}
	if app.kept == nil {
		app.kept = map[string]*browse{}
	}
	page := app.Page
	if appPage(page) {
		page = app.srcPage
	}
	app.kept[app.Src.Info().ID] = &browse{
		src: app.Src, nav: app.Nav, navErr: app.NavErr, page: page,
		cols: app.cols, stack: app.Stack, search: app.Search, warmed: app.warmed,
	}
}

// show makes s the source shown, with b what it showed before, nil for
// nothing.
func (app *App) show(s source.Source, b *browse) {
	app.Src = s
	app.LogOf = ""
	app.NavBusy = false
	if b == nil {
		b = &browse{}
	}
	app.Nav, app.NavErr, app.cols, app.Stack, app.Search, app.warmed = b.nav, b.navErr, b.cols, b.stack, b.search, b.warmed
	if app.cols == nil {
		app.cols = map[string]*Collection{}
	}
	if app.Search == nil {
		app.Search = &searchState{}
	}
	app.Page, app.srcPage = b.page, b.page
	if s == nil {
		return
	}
	app.Lib.Source = s.Info().ID
	app.Lib.MarkDirty()
	if app.Nav == nil || app.NavErr != "" {
		app.NavErr = ""
		app.LoadNav()
	} else if app.Page == "" {
		app.Page = startPage(app.Nav, "", app.HasCap(source.CapSearch))
		app.srcPage = app.Page
	}
	if app.Page != "" {
		app.Lib.Page = app.Page // else LoadNav saves the first
	}
}

// startPage is the page to show of a source with navigation nav: want,
// if it has it, or its first.
func startPage(nav []source.NavItem, want string, search bool) string {
	if want == PageSearch && search {
		return want
	}
	for _, it := range nav {
		if it.ID == want {
			return want
		}
	}
	if len(nav) > 0 {
		return nav[0].ID
	}
	return ""
}

// DevSource tells s is the developer mode's plugin.
func (app *App) DevSource(s source.Source) bool {
	return s != nil && app.Dev.Plugin != nil && s == source.Source(app.Dev.Plugin)
}
