package app

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// After the first page, the app warms a few other pages of the source one
// at a time, so the calls the user makes never queue behind them; a page
// that failed to warm loads again when shown, without an error.
func TestWarmIsLimited(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	var inFlight, most atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	nav := []string{"n1", "n2", "n3", "n4", "n5", "n6"}
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Capabilities: []source.Capability{source.CapSearch, source.CapSearchHome}},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			items := make([]source.NavItem, len(nav))
			for i, n := range nav {
				items[i] = source.NavItem{ID: n, Title: n}
			}
			return items, nil
		},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			mu.Lock()
			asked = append(asked, r.ID)
			mu.Unlock()
			if r.ID == "n1" {
				return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: "x", Title: "x"}}}, nil
			}
			if fail.Load() && !source.IsBackground(ctx) {
				t.Errorf("warming %s as the user's", r.ID)
			}
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			time.Sleep(30 * time.Millisecond)
			if fail.Load() {
				return nil, source.NewError(source.ErrNetwork, "网络不好", nil)
			}
			return &source.CollectionPage{Items: []source.Card{{Kind: source.CardMedia, ID: r.ID, Title: r.ID}}}, nil
		},
		OnSearchHome: func(ctx context.Context) (*source.CollectionPage, error) {
			if fail.Load() && !source.IsBackground(ctx) {
				t.Error("warming the search home as the user's")
			}
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			time.Sleep(30 * time.Millisecond)
			if fail.Load() {
				return nil, source.NewError(source.ErrNetwork, "网络不好", nil)
			}
			return &source.CollectionPage{}, nil
		},
	}
	app, run := cancelApp(t, src)
	app.warmed = false
	app.UseSource(src)
	run(5 * time.Second)
	app.Ensure(app.Col(app.Page))
	for run(500 * time.Millisecond) {
	}
	mu.Lock()
	got := slices.Clone(asked)
	mu.Unlock()
	if !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Errorf("asked for %v, want the first page and two more", got)
	}
	if m := most.Load(); m > 1 {
		t.Errorf("%d warming calls at once", m)
	}
	cs := app.Col("n2")
	if cs.Results.Err != "" || cs.Results.Loaded {
		t.Fatalf("a page that failed to warm: err %q, loaded %v", cs.Results.Err, cs.Results.Loaded)
	}
	if app.Search.HotErr != "" || app.Search.Hot != nil {
		t.Errorf("a search home that failed to warm: err %q", app.Search.HotErr)
	}
	fail.Store(false)
	app.Go("n2")
	app.Ensure(cs)
	for run(300 * time.Millisecond) {
	}
	if len(cs.Results.Items) != 1 {
		t.Errorf("shown, the page has %d items", len(cs.Results.Items))
	}
	if app.ShowSearch(); app.Search.Hot == nil {
		for run(300 * time.Millisecond) {
		}
	}
	if app.Search.Hot == nil {
		t.Error("shown, the search home didn't load")
	}
}
