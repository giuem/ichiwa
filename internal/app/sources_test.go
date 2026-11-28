package app

import (
	"context"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

func navSource(id string, nav ...string) *sourcetest.Source {
	items := make([]source.NavItem, len(nav))
	for i, n := range nav {
		items[i] = source.NavItem{ID: n, Title: n}
	}
	return &sourcetest.Source{
		InfoData: source.Info{ID: id, Name: id, Capabilities: []source.Capability{source.CapSearch}},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			return items, nil
		},
		OnCollection: func(_ context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: id + "-" + r.ID, Title: r.ID}}}, nil
		},
	}
}

// Each source shows again where the user left it: its page, its opened
// collections, its search; what loads for the one left stops.
func TestSwitchSourceKeepsPlace(t *testing.T) {
	a, b := navSource("a", "a1", "a2"), navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	if app.Page != "a1" {
		t.Fatalf("page = %q", app.Page)
	}
	app.Go("a2")
	app.OpenCollection(source.Card{Kind: source.CardCollection, ID: "person:1", Title: "人"})
	app.Search.Query = "兔子"

	app.SwitchSource(b)
	run(5 * time.Second)
	if app.Src != b || app.Page != "b1" || len(app.Stack) != 0 || app.Search.Query != "" || app.Lib.Source != "b" {
		t.Fatalf("b: page %q, stack %d, query %q", app.Page, len(app.Stack), app.Search.Query)
	}
	app.Go(PageHistory)

	app.SwitchSource(a)
	if app.Src != a || app.Page != "a2" || len(app.Stack) != 1 || app.Search.Query != "兔子" {
		t.Fatalf("back to a: page %q, stack %d, query %q", app.Page, len(app.Stack), app.Search.Query)
	}
	app.SwitchSource(b)
	if app.Page != "b1" {
		t.Fatalf("back to b from the history: page %q", app.Page)
	}
	for run(100 * time.Millisecond) {
	}
}

// A source turned off is not offered, gives way to another while shown,
// and the app shows none when none is on.
func TestSourceOff(t *testing.T) {
	a, b := navSource("a", "a1"), navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)

	app.SetSourceOn("a", false)
	run(5 * time.Second)
	if app.Src != b || app.Page != PageSources || !app.Lib.Off["a"] {
		t.Fatalf("shown %v, page %q, off %v", app.Src, app.Page, app.Lib.Off)
	}
	if on := app.SourcesOn(); len(on) != 1 || on[0] != b {
		t.Fatalf("on = %v", on)
	}
	app.SetSourceOn("b", false)
	if app.Src != nil || app.Nav != nil || app.HasCap(source.CapSearch) {
		t.Fatalf("shown %v with none on", app.Src)
	}
	app.SetSourceOn("a", true)
	run(5 * time.Second)
	if app.Src != a || app.Page != PageSources || len(app.Nav) != 1 {
		t.Fatalf("turned back on: shown %v, page %q", app.Src, app.Page)
	}
	app.Go(PageHistory)
	app.SwitchSource(a)
	if app.Page != "a1" {
		t.Fatalf("page = %q", app.Page)
	}
}

// An actor on the page of a drama of another source opens over that
// source's pages, which then shows.
func TestOpenCollectionOfAnotherSource(t *testing.T) {
	a, b := navSource("a", "a1"), navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	app.Go(PageHistory)
	app.OpenCollectionOf("b", source.Card{Kind: source.CardCollection, ID: "person:1", Title: "人"})
	run(5 * time.Second)
	if app.Src != b || len(app.Stack) != 1 || app.Page != "b1" {
		t.Fatalf("shown %v, stack %d, page %q", app.Src, len(app.Stack), app.Page)
	}
	app.Lib.Off["a"] = true
	app.OpenCollectionOf("a", source.Card{Kind: source.CardCollection, ID: "person:2", Title: "人"})
	if app.Src != b || app.Unavailable == nil || app.Unavailable.Source != "a" {
		t.Fatalf("shown %v, unavailable %+v", app.Src, app.Unavailable)
	}
	for run(100 * time.Millisecond) {
	}
}
