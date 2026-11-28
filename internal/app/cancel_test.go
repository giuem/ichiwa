package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

func cancelApp(t *testing.T, src source.Source) (*App, func(time.Duration) bool) {
	t.Helper()
	done := make(chan func(), 64)
	app := &App{
		src:     src,
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
	app.loadMore(navFeed(app, "home"))

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
	if cs := app.cols["home"]; cs != nil && len(cs.results.items) != 0 {
		t.Fatalf("b's feed took a's items: %+v", cs.results.items)
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
	app.page = "home"
	f := navFeed(app, "home")
	app.loadMore(f)
	wait()
	app.go2("rank")
	if !run(5 * time.Second) {
		t.Fatal("the canceled load never came back")
	}
	if canceled.Load() != 1 || f.err != "" || f.loading || f.loaded || !f.more {
		t.Fatalf("canceled feed: canceled %d, %+v", canceled.Load(), f)
	}

	// The person page, left for the page under it.
	app.openCollection(Card{Kind: source.CardCollection, ID: "p", Title: "人"})
	cs := app.stack[0]
	app.ensure(cs)
	wait()
	app.closeCollection()
	run(5 * time.Second)
	if canceled.Load() != 2 || cs.results.err != "" {
		t.Fatalf("person page: canceled %d, err %q", canceled.Load(), cs.results.err)
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
	app.go2("home")
	app.loadMore(f)
	run(5 * time.Second)
	if f.err == "" {
		t.Fatal("a source's own Canceled shows no error")
	}
}

// A list the source rate-limited waits out retryAfter before asking
// again, counting down in its error (protocol §10).
func TestRateLimitedListWaits(t *testing.T) {
	var calls atomic.Int32
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			calls.Add(1)
			e := source.NewError(source.ErrRateLimited, "请求太频繁", nil)
			e.RetryAfter = 30 * time.Second
			return nil, e
		},
	}
	app, run := cancelApp(t, src)
	f := navFeed(app, "home")
	app.loadMore(f)
	run(5 * time.Second)
	if got := f.errText(); got != "请求太频繁（30 秒后可以重试）" {
		t.Fatalf("error = %q", got)
	}
	// A retry meanwhile asks nothing and keeps the error.
	f.err = ""
	app.loadMore(f)
	if calls.Load() != 1 || f.loading || f.err == "" {
		t.Fatalf("retry during the wait: calls %d, %+v", calls.Load(), f)
	}
	// Once the wait is over, it asks again.
	f.retryAt = time.Now().Add(-time.Second)
	f.err = ""
	app.loadMore(f)
	run(5 * time.Second)
	if calls.Load() != 2 {
		t.Fatalf("after the wait: calls %d", calls.Load())
	}
}

// A person page whose list failed keeps the header it came with, and its
// works show the error, to retry.
func TestPersonPageKeepsHeaderOfFailedList(t *testing.T) {
	var calls atomic.Int32
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if calls.Add(1) == 1 {
				return &source.CollectionPage{Header: &source.Header{Title: "演员甲", Description: "简介"}},
					source.NewError(source.ErrNetwork, "列表暂时取不到", nil)
			}
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "m", Title: "剧"}}}, nil
		},
	}
	app, run := cancelApp(t, src)
	app.openCollection(Card{Kind: source.CardCollection, ID: "p", Title: "甲"})
	cs := app.stack[0]
	app.ensure(cs)
	run(5 * time.Second)
	if cs.header == nil || cs.header.Title != "演员甲" || cs.results.err != "列表暂时取不到" {
		t.Fatalf("header %+v, works err %q", cs.header, cs.results.err)
	}
	cs.results.err = ""
	app.loadMore(&cs.results)
	run(5 * time.Second)
	if len(cs.results.items) != 1 || cs.header == nil || cs.header.Title != "演员甲" {
		t.Fatalf("retry: %+v, header %+v", cs.results.items, cs.header)
	}
}

// blockingSource is a source whose collections and searches wait
// for their context until open, then answer with one card each.
func blockingSource(t *testing.T) (src *sourcetest.Source, open *atomic.Bool, calls *atomic.Int32, entered chan struct{}) {
	open, calls, entered = new(atomic.Bool), new(atomic.Int32), make(chan struct{}, 16)
	page := func(ctx context.Context, id string) (*source.CollectionPage, error) {
		calls.Add(1)
		if !open.Load() {
			entered <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: id, Title: "剧", Image: "x", Badges: []string{"全1集"}}}}, nil
	}
	src = &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapSearch}},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			return page(ctx, "c-"+r.ID)
		},
		OnSearch: func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
			return page(ctx, "s-"+r.Query)
		},
	}
	return src, open, calls, entered
}

func waitEntered(t *testing.T, entered chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never started")
	}
}

// The ranking canceled as its page is left loads again when shown, and
// the one warm loads for pages to come isn't canceled with the page.
func TestRankLoadsAgainAfterCancel(t *testing.T) {
	src, open, calls, entered := blockingSource(t)
	app, run := cancelApp(t, src)
	app.page = "rank"
	tt := ui.NewTester(func(c *ui.Context) { app.collectionPage(c, app.col("rank"), false) }, 900, 600)
	tt.Frame()
	waitEntered(t, entered)
	app.go2("home")
	if !run(5 * time.Second) {
		t.Fatal("the canceled load never came back")
	}
	l := &app.col("rank").results
	if l.loaded || l.loading || l.err != "" {
		t.Fatalf("canceled ranking: %+v", l)
	}
	open.Store(true)
	app.go2("rank")
	tt.Frame()
	if !run(5*time.Second) || len(l.items) != 1 || calls.Load() != 2 {
		t.Fatalf("ranking shown again: calls %d, %+v", calls.Load(), l)
	}

	// warm, for the pages to come, outlives the page shown.
	src2, _, _, entered2 := blockingSource(t)
	app2, run2 := cancelApp(t, src2)
	app2.warmed = false
	app2.page = "home"
	app2.nav = []source.NavItem{{ID: "home"}, {ID: "live"}, {ID: "comic"}, {ID: "rank"}}
	app2.warm()
	for range 4 { // every page of the navigation
		waitEntered(t, entered2)
	}
	app2.go2(pageSearch)
	if run2(300 * time.Millisecond) {
		t.Fatal("leaving the page canceled what warm loads")
	}
	if l := &app2.col("rank").results; l.err != "" || !l.loading {
		t.Fatalf("warm ranking: %+v", l)
	}
	app2.leaveSource()
	for run2(time.Second) {
	}
}

// A search canceled as its page is left searches again when shown,
// rather than say nothing was found.
func TestSearchLoadsAgainAfterCancel(t *testing.T) {
	src, open, calls, entered := blockingSource(t)
	app, run := cancelApp(t, src)
	app.page = pageSearch
	app.runSearch("兔子")
	waitEntered(t, entered)
	app.go2("home")
	if !run(5 * time.Second) {
		t.Fatal("the canceled search never came back")
	}
	r := &app.search.results
	if r.loaded || r.loading || r.err != "" {
		t.Fatalf("canceled search: %+v", r)
	}
	open.Store(true)
	app.go2(pageSearch)
	tt := ui.NewTester(app.searchPage, 900, 600)
	tt.Frame()
	if !run(5*time.Second) || len(r.items) != 1 || calls.Load() != 2 {
		t.Fatalf("search shown again: calls %d, %+v", calls.Load(), r)
	}
}

// Covers asked for as a page is left still come, for the cards shown
// when it is shown again.
func TestCoversComeAfterPageLeft(t *testing.T) {
	release, refreshing := make(chan struct{}), make(chan struct{}, 1)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapRefresh}},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "m", Title: "剧"}}}, nil
		},
		OnRefresh: func(ctx context.Context, ids []string) ([]source.Card, error) {
			refreshing <- struct{}{}
			<-release
			return []source.Card{{Kind: source.CardMedia, ID: "m", Image: "cover", Badges: []string{"全1集"}}}, ctx.Err()
		},
	}
	app, run := cancelApp(t, src)
	app.page = "home"
	f := navFeed(app, "home")
	app.loadMore(f)
	run(5 * time.Second)
	waitEntered(t, refreshing)
	app.go2("rank")
	close(release)
	run(5 * time.Second)
	if len(f.items) != 1 || f.items[0].Image != "cover" {
		t.Fatalf("items = %+v", f.items)
	}
}

// Searching anew, or refreshing a feed, cancels the calls for what it
// showed before: its next page and its covers.
func TestResetCancelsFeedCalls(t *testing.T) {
	canceled := make(chan string, 4)
	entered := make(chan struct{}, 4)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapSearch, source.CapRefresh}},
		OnSearch: func(ctx context.Context, r source.SearchRequest) (*source.CollectionPage, error) {
			if r.Query == "后来" {
				return &source.CollectionPage{}, nil
			}
			entered <- struct{}{}
			<-ctx.Done()
			canceled <- "search"
			return nil, ctx.Err()
		},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "m", Title: "剧"}}}, nil
		},
		OnRefresh: func(ctx context.Context, ids []string) ([]source.Card, error) {
			entered <- struct{}{}
			<-ctx.Done()
			canceled <- "refresh"
			return nil, ctx.Err()
		},
	}
	app, run := cancelApp(t, src)
	app.runSearch("先前")
	waitEntered(t, entered)
	app.runSearch("后来")
	select {
	case what := <-canceled:
		if what != "search" {
			t.Fatalf("canceled %s", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("searching anew left the earlier search running")
	}

	f := navFeed(app, "home")
	app.loadMore(f)
	for f.loading {
		run(5 * time.Second)
	}
	waitEntered(t, entered)
	f.reset()
	select {
	case what := <-canceled:
		if what != "refresh" {
			t.Fatalf("canceled %s", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refreshing the feed left its covers loading")
	}
	for run(300 * time.Millisecond) {
	}
}

// navFeed is the feed of the page id of the navigation, ready to load.
func navFeed(app *App, id string) *Feed {
	cs := app.col(id)
	cs.results.setFetch(app.src, collectionFetch(source.CollectionRequest{ID: id}))
	cs.results.onPage = cs.applyPage
	return &cs.results
}
