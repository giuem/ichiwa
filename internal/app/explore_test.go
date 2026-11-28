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

// Choosing a new option drops the results and loads under way; choosing the
// same option again changes nothing; applyPage takes the rows the source
// returns, with their choices.
func TestCollectionChoose(t *testing.T) {
	cs := newCol("filter", "筛选")
	cs.results.setFetch(&sourcetest.Source{}, func(context.Context, source.Source, string) (feedPage, error) { return feedPage{}, nil })
	if !cs.choose("theme", "t5") || cs.chosen["theme"] != "t5" {
		t.Fatalf("choose theme: %+v", cs.chosen)
	}
	if cs.results.fetch != nil || cs.results.gen != 1 {
		t.Error("a new choice should drop the old results and their fetch")
	}
	cs.results.loaded = true
	if cs.choose("theme", "t5") || !cs.results.loaded {
		t.Error("choosing the same option again reset the results")
	}
	cs.choose("genre", "comic")
	cs.applyPage(feedPage{page: &source.CollectionPage{
		Filters: []FilterRow{
			{Key: "genre", Title: "体裁", Value: "comic", Options: []source.FilterOption{{Title: "全部体裁"}, {Value: "comic", Title: "漫剧"}}},
			{Key: "theme", Title: "题材", Options: []source.FilterOption{{Title: "全部题材"}}},
		},
	}})
	if len(cs.chosen) != 1 || cs.chosen["genre"] != "comic" {
		t.Errorf("applyPage: %v", cs.chosen)
	}
	if !cs.choose("genre", "") || len(cs.chosen) != 0 {
		t.Errorf("all: %v", cs.chosen)
	}
	cs.choose("genre", "comic")
	cs.clear()
	if len(cs.chosen) != 0 {
		t.Errorf("clear: %v", cs.chosen)
	}
}

func collectionApp(t *testing.T, src source.Source, post func(func())) *App {
	t.Helper()
	return &App{
		src:     src,
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		warmed:  true,
		post:    post,
	}
}

func drainer(t *testing.T, done chan func()) func() {
	return func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}
}

// A first page whose rows came and list failed shows the rows, and a retry
// loads the list.
func TestFilterRowsShowWhenListFails(t *testing.T) {
	done := make(chan func(), 8)
	var calls atomic.Int32
	rows := []FilterRow{{Key: "genre", Title: "体裁", Options: []source.FilterOption{{Title: "全部体裁"}, {Value: "comic", Title: "漫剧"}}}}
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(context.Context, source.CollectionRequest) (*source.CollectionPage, error) {
			if calls.Add(1) == 1 {
				return &source.CollectionPage{Filters: rows}, source.NewError(source.ErrNetwork, "网络连接失败", nil)
			}
			return &source.CollectionPage{Filters: rows, Items: []Card{{Kind: source.CardMedia, ID: "m1", Title: "好剧"}}}, nil
		},
	}
	app := collectionApp(t, src, func(fn func()) { done <- fn })
	drain := drainer(t, done)
	cs := app.col("filter")
	tt := ui.NewTester(func(c *ui.Context) { app.collectionPage(c, cs, false) }, 1100, 700)
	tt.Frame()
	drain()
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleRadio, "漫剧"); !ok {
		t.Fatal("the rows don't show when the list failed")
	}
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("no retry: %v", err)
	}
	tt.Frame()
	drain()
	tt.Frame()
	if cs.results.err != "" || len(cs.results.items) != 1 {
		t.Fatalf("after retry: err=%q items=%+v", cs.results.err, cs.results.items)
	}
}

// Switching a ranking's tab hides the boards of the tab before until the
// new page brings its own.
func TestRankSwitchCategoryClearsSubBoards(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{}, func(func()) {})
	cs := app.col("rank")
	cs.first = &source.CollectionPage{Layout: source.LayoutRanked}
	cs.haveFirst = true
	cs.rows = []FilterRow{
		{Key: "content", Title: "榜单类别", Value: "human", Options: []source.FilterOption{{Value: "human", Title: "真人剧"}, {Value: "actors", Title: "演员"}}},
		{Key: "board", Title: "榜单", Value: "hot", Options: []source.FilterOption{{Value: "hot", Title: "热播榜"}, {Value: "new", Title: "新剧榜"}}},
	}
	cs.chosen = map[string]string{"content": "human", "board": "hot"}
	cs.results.setFetch(&sourcetest.Source{}, func(context.Context, source.Source, string) (feedPage, error) {
		return feedPage{}, context.Canceled
	})
	cs.results.loaded = true
	cs.results.items = []Card{{Kind: source.CardMedia, ID: "m1", Title: "热播剧"}}
	tt := ui.NewTester(func(c *ui.Context) { app.collectionPage(c, cs, false) }, 1100, 700)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleRadio, "热播榜"); !ok {
		t.Fatal("expected the board 热播榜")
	}
	if a := accessOf(t, tt, ui.RoleButton, "热播剧"); a.Description != "第1名" {
		t.Errorf("a ranked card says %q", a.Description)
	}
	if err := tt.Click("演员"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(cs.rows) != 1 || cs.chosen["content"] != "actors" {
		t.Fatalf("rows %+v, chosen %v", cs.rows, cs.chosen)
	}
	if _, ok := tt.AccessByRole(ui.RoleRadio, "热播榜"); ok {
		t.Fatal("the board of the tab before still shows")
	}
}

// An actor on a ranking opens their page over it, and its back button
// returns.
func TestActorOpensCollection(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{}, func(func()) {})
	app.page = "rank"
	cs := app.col("rank")
	cs.first = &source.CollectionPage{Layout: source.LayoutRanked}
	cs.results.setFetch(&sourcetest.Source{}, func(context.Context, source.Source, string) (feedPage, error) { return feedPage{}, nil })
	cs.results.loaded = true
	cs.results.items = []Card{{Kind: source.CardCollection, ID: "person:u1", Title: "李柯以", Shape: source.ShapeCircle, Note: "1060万人气"}}
	tt := ui.NewTester(func(c *ui.Context) {
		if n := len(app.stack); n > 0 {
			// Its page shows once its data is in, so nothing is fetched.
			top := app.stack[n-1]
			top.results.loaded = true
			top.first = &source.CollectionPage{}
			top.header = &source.Header{Title: "李柯以", Meta: []source.Meta{{Value: "女演员"}}}
		}
		app.content(c)
	}, 1100, 700)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleButton, "李柯以"); a.Description != "第1名，1060万人气" {
		t.Errorf("the actor says %q", a.Description)
	}
	if err := tt.Click("李柯以"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(app.stack) != 1 || app.stack[0].id != "person:u1" {
		t.Fatalf("stack = %+v", app.stack)
	}
	accessOf(t, tt, ui.RoleHeading, "李柯以")
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.stack) != 0 {
		t.Fatal("back didn't close the actor's page")
	}
}

// An opened collection whose first page failed shows the card's name with
// the error, and a retry brings its header.
func TestOpenedCollectionRetries(t *testing.T) {
	done := make(chan func(), 8)
	var fail atomic.Bool
	fail.Store(true)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(context.Context, source.CollectionRequest) (*source.CollectionPage, error) {
			if fail.Load() {
				return nil, source.NewError(source.ErrNetwork, "加载失败了", nil)
			}
			return &source.CollectionPage{
				Header: &source.Header{Title: "李柯以", Description: "演员简介文本"},
				Items:  []Card{{Kind: source.CardMedia, ID: "s1", Title: "代表作"}},
			}, nil
		},
	}
	app := collectionApp(t, src, func(fn func()) { done <- fn })
	drain := drainer(t, done)
	app.openCollection(Card{Kind: source.CardCollection, ID: "person:u1", Title: "李柯以", Image: "x", Shape: source.ShapeCircle})
	tt := ui.NewTester(app.content, 1100, 700)
	tt.Frame()
	drain()
	tt.Frame()
	accessOf(t, tt, ui.RoleHeading, "李柯以")
	fail.Store(false)
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("no retry: %v", err)
	}
	tt.Frame()
	drain()
	tt.Frame()
	cs := app.stack[0]
	if cs.header == nil || cs.header.Description != "演员简介文本" || len(cs.results.items) != 1 {
		t.Fatalf("after retry: header %+v, items %+v", cs.header, cs.results.items)
	}
	accessOf(t, tt, ui.RoleButton, "代表作")
}

// Refreshing a page drops its sections, canceling their calls, and leaving
// an opened collection cancels what it was still loading.
func TestRefreshAndLeaveCancelSections(t *testing.T) {
	canceled := make(chan string, 4)
	entered := make(chan struct{}, 4)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if r.ID == "home" {
				return &source.CollectionPage{Sections: []source.Section{{Title: "分区", Collection: "sec"}}}, nil
			}
			entered <- struct{}{}
			<-ctx.Done()
			canceled <- r.ID
			return nil, ctx.Err()
		},
	}
	app, run := cancelApp(t, src)
	cs := app.col("home")
	app.ensure(cs)
	run(5 * time.Second)
	app.sectionFeed(cs.sections, src, "sec")
	waitEntered(t, entered)
	cs.refresh()
	select {
	case id := <-canceled:
		if id != "sec" {
			t.Fatalf("canceled %s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refreshing left the section loading")
	}
	for run(300 * time.Millisecond) {
	}

	app.openCollection(Card{Kind: source.CardCollection, ID: "person:p", Title: "人"})
	app.ensure(app.stack[0])
	waitEntered(t, entered)
	app.closeCollection()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("leaving the page left its list loading")
	}
}
