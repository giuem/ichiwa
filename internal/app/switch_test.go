package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// waitCall waits for the call want to start.
func waitCall(t *testing.T, entered chan string, want string) {
	t.Helper()
	select {
	case got := <-entered:
		if got != want {
			t.Fatalf("called %s, want %s", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never started", want)
	}
}

// A search page left loading for another source loads again when the
// source shows again: its search home, its results and their next page.
func TestSearchLoadingAcrossSwitch(t *testing.T) {
	entered := make(chan string, 8)
	var block atomic.Bool
	block.Store(true)
	a := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapSearch, source.CapSearchHome}},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			return []source.NavItem{{ID: "a1", Title: "a1"}}, nil
		},
		OnSearchHome: func(ctx context.Context) (*source.CollectionPage, error) {
			entered <- "home"
			if block.Load() {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &source.CollectionPage{Sections: []source.Section{{Title: "热搜"}}}, nil
		},
		OnSearch: func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
			entered <- "search"
			if block.Load() {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "x", Title: "x"}}}, nil
		},
	}
	b := navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	app.Go(PageSearch)
	app.LoadHot()
	waitCall(t, entered, "home")
	app.RunSearch("兔")
	waitCall(t, entered, "search")

	app.SwitchSource(b)
	for run(300 * time.Millisecond) {
	}
	block.Store(false)
	app.SwitchSource(a)
	if app.Page != PageSearch {
		t.Fatalf("page %q", app.Page)
	}
	app.LoadHot()
	app.ShowSearch()
	for run(300 * time.Millisecond) {
	}
	if app.Search.Hot == nil || app.Search.hotBusy {
		t.Errorf("the search home stays loading: hot %v, busy %v", app.Search.Hot, app.Search.hotBusy)
	}
	if r := app.Search.Results; r.Loading || len(r.Items) != 1 {
		t.Errorf("the results stay loading: loading %v, items %d", r.Loading, len(r.Items))
	}
}
