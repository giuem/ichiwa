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

// Suggestions wait for typing to rest: only the last of quick keystrokes
// asks the source, and a search run asks nothing more.
func TestSuggestWaitsForTyping(t *testing.T) {
	var calls atomic.Int32
	var asked atomic.Value
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapSearch, source.CapSearchSuggest}},
		OnSuggest: func(ctx context.Context, q string) ([]string, error) {
			calls.Add(1)
			asked.Store(q)
			return []string{q + "一"}, nil
		},
	}
	app, run := cancelApp(t, src)
	for _, q := range []string{"兔", "兔子", "兔子的", "兔子的家", "兔子的家人"} {
		app.Typed(q)
	}
	for run(suggestWait + 300*time.Millisecond) {
	}
	if n := calls.Load(); n != 1 || asked.Load() != "兔子的家人" {
		t.Errorf("asked %d times, last for %v", n, asked.Load())
	}
	if app.Search.SuggestFor != "兔子的家人" {
		t.Errorf("suggests for %q", app.Search.SuggestFor)
	}

	app.Typed("猫")
	app.RunSearch("猫")
	for run(suggestWait + 300*time.Millisecond) {
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("a search run still asked for suggestions: %d calls", n)
	}
}

// Turning off the source shown goes to the next where the user left it.
func TestSourceOffKeepsNextPlace(t *testing.T) {
	a, b := navSource("a", "a1"), navSource("b", "b1", "b2")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(b)
	run(5 * time.Second)
	app.Go("b2")
	app.SwitchSource(a)
	run(5 * time.Second)
	app.SetSourceOn("a", false)
	if app.Src != b || app.Page != PageSources {
		t.Fatalf("shown %v on %q", app.Src, app.Page)
	}
	app.SwitchSource(b)
	if app.Page != "b2" {
		t.Errorf("b shows %q", app.Page)
	}
	if app.kept["a"] != nil {
		t.Error("the source turned off is kept")
	}
}

// Covers still coming when the user switches away come when the source
// shows again.
func TestCoversCompleteAfterSwitch(t *testing.T) {
	var block atomic.Bool
	block.Store(true)
	entered := make(chan string, 8)
	a := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapRefresh}},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			return []source.NavItem{{ID: "a1", Title: "a1"}}, nil
		},
		OnCollection: func(context.Context, source.CollectionRequest) (*source.CollectionPage, error) {
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "m", Title: "m"}}}, nil
		},
		OnRefresh: func(ctx context.Context, ids []string) ([]source.Card, error) {
			entered <- "refresh"
			if block.Load() {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []source.Card{{Kind: source.CardMedia, ID: "m", Image: "cover", Badges: []string{"全10集"}}}, nil
		},
	}
	b := navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	app.Ensure(app.Col("a1"))
	run(5 * time.Second)
	waitCall(t, entered, "refresh")
	app.SwitchSource(b)
	for run(300 * time.Millisecond) {
	}
	block.Store(false)
	app.SwitchSource(a)
	for run(300 * time.Millisecond) {
	}
	if it := app.Col("a1").Results.Items; len(it) != 1 || it[0].Image != "cover" {
		t.Errorf("items = %+v", it)
	}
}

// The pages of a source left are saved for the next launch too.
func TestSaveFeedsOfSourcesLeft(t *testing.T) {
	a, b := navSource("a", "a1"), navSource("b", "b1")
	app, run := cancelApp(t, a)
	app.CacheDir = t.TempDir()
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	app.Ensure(app.Col("a1"))
	run(5 * time.Second)
	app.SwitchSource(b)
	run(5 * time.Second)
	app.SaveFeeds(true)
	if c := loadFeedCache(feedCachePath(app.CacheDir, "a", "a1")); c == nil || len(c.Items) != 1 {
		t.Errorf("a's page wasn't saved: %+v", c)
	}
}
