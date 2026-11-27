package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

func cancelApp(t *testing.T, src source.Source) (*App, func(time.Duration) bool) {
	t.Helper()
	done := make(chan func(), 64)
	app := &App{
		src:     src,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		warmed:  true,
		post:    func(fn func()) { done <- fn },
	}
	app.sources.add(src)
	// run runs what comes back within d; false when nothing did.
	run := func(d time.Duration) bool {
		select {
		case fn := <-done:
			fn()
			return true
		case <-time.After(d):
			return false
		}
	}
	return app, run
}

// What is loading for one source never goes to the one the app switches
// to: not the next page of a search, nor the covers of its cards.
func TestSwitchingSourceKeepsCallsApart(t *testing.T) {
	caps := []source.Capability{source.CapSearch, source.CapRefresh}
	release := make(chan struct{})
	a := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: caps},
		OnSearch: func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "a1", Title: "甲"}}, Cursor: "a-next"}, nil
		},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			<-release
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "a2", Title: "乙"}}}, nil
		},
	}
	var leaked atomic.Int32
	b := &sourcetest.Source{
		InfoData: source.Info{ID: "b", Capabilities: caps},
		OnSearch: func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
			t.Errorf("b searched for %q at %q", r.Query, r.Cursor)
			leaked.Add(1)
			return nil, nil
		},
		OnRefresh: func(ctx context.Context, ids []string) ([]source.Card, error) {
			t.Errorf("b refreshed %v", ids)
			leaked.Add(1)
			return nil, nil
		},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if r.ID == "home" {
				return &source.CollectionPage{}, nil
			}
			t.Errorf("b got collection %+v", r)
			return nil, nil
		},
	}
	app, run := cancelApp(t, a)
	app.sources.add(b)

	// A search of a whose next page waits a while, as after a page of
	// repeats, when the app switches to b.
	app.runSearch("红果的词")
	run(5 * time.Second)
	if len(app.search.results.items) != 1 {
		t.Fatalf("search = %+v", app.search.results)
	}
	app.search.results.stalls = 1
	app.loadMore(&app.search.results)
	// A page of a whose cards lack covers, still loading.
	app.loadMore(app.feed(pageRecommend))

	app.useSource(b)
	close(release)
	for run(stallWait + 300*time.Millisecond) {
	}
	if leaked.Load() != 0 {
		t.Fatal("a's calls went to b")
	}
	if len(app.search.results.items) != 0 || app.search.results.err != "" {
		t.Fatalf("b's search state took a's results: %+v", app.search.results)
	}
	if f := app.feeds[pageRecommend]; f != nil && len(f.items) != 0 {
		t.Fatalf("b's feed took a's items: %+v", f.items)
	}
}

// Leaving a page cancels its calls (§9): no error shows, and the page
// loads again when shown. A source that fails with Canceled by itself
// shows an error.
func TestLeavingPageCancelsCalls(t *testing.T) {
	var canceled atomic.Int32
	var ownCancel atomic.Bool
	entered := make(chan struct{}, 8)
	wait := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("the call never started")
		}
	}
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if ownCancel.Load() {
				return nil, context.Canceled
			}
			entered <- struct{}{}
			<-ctx.Done()
			canceled.Add(1)
			return nil, ctx.Err()
		},
		OnMedia: func(ctx context.Context, id string) (*source.Media, error) {
			entered <- struct{}{}
			<-ctx.Done()
			canceled.Add(1)
			return nil, ctx.Err()
		},
	}
	app, run := cancelApp(t, src)
	app.page = pageRecommend
	f := app.feed(pageRecommend)
	app.loadMore(f)
	wait()
	app.go2(pageRank)
	if !run(5 * time.Second) {
		t.Fatal("the canceled load never came back")
	}
	if canceled.Load() != 1 || f.err != "" || f.loading || f.loaded || !f.more {
		t.Fatalf("canceled feed: canceled %d, %+v", canceled.Load(), f)
	}

	// The person page, left for the page under it.
	app.openPerson(Card{Kind: source.CardCollection, ID: "p", Title: "人"})
	app.loadPerson(app.person)
	ps := app.person
	wait()
	app.closePerson()
	run(5 * time.Second)
	if canceled.Load() != 2 || ps.headerErr != "" {
		t.Fatalf("person page: canceled %d, err %q", canceled.Load(), ps.headerErr)
	}

	// The player page, closed while the details load.
	app.open("a", Card{Kind: source.CardMedia, ID: "m", Title: "剧"}, -1)
	w := app.watch
	wait()
	w.close()
	app.watch = nil
	run(5 * time.Second)
	if canceled.Load() != 3 || w.detailErr != "" {
		t.Fatalf("player: canceled %d, err %q", canceled.Load(), w.detailErr)
	}

	// Canceled by the source itself is an error.
	ownCancel.Store(true)
	app.go2(pageRecommend)
	app.loadMore(f)
	run(5 * time.Second)
	if f.err == "" {
		t.Fatal("a source's own Canceled shows no error")
	}
}
