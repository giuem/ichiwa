package ui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

func collectionApp(t *testing.T, src source.Source, post func(func())) *App {
	t.Helper()
	return &App{App: core.Headless(testLibrary(t), post, src), pal: lightPalette(), posters: newPosters(t.TempDir(), func() {})}
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
	cs := app.Col("filter")
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
	if cs.Results.Err != "" || len(cs.Results.Items) != 1 {
		t.Fatalf("after retry: err=%q items=%+v", cs.Results.Err, cs.Results.Items)
	}
}

// Switching a ranking's tab hides the boards of the tab before until the
// new page brings its own.
func TestRankSwitchCategoryClearsSubBoards(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{}, func(func()) {})
	cs := app.Col("rank")
	cs.First = &source.CollectionPage{Layout: source.LayoutRanked}
	cs.Rows = []FilterRow{
		{Key: "content", Title: "榜单类别", Value: "human", Options: []source.FilterOption{{Value: "human", Title: "真人剧"}, {Value: "actors", Title: "演员"}}},
		{Key: "board", Title: "榜单", Value: "hot", Options: []source.FilterOption{{Value: "hot", Title: "热播榜"}, {Value: "new", Title: "新剧榜"}}},
	}
	cs.Chosen = map[string]string{"content": "human", "board": "hot"}
	cs.Results.Loaded = true
	cs.Results.Items = []Card{{Kind: source.CardMedia, ID: "m1", Title: "热播剧"}}
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
	if len(cs.Rows) != 1 || cs.Chosen["content"] != "actors" {
		t.Fatalf("rows %+v, chosen %v", cs.Rows, cs.Chosen)
	}
	if _, ok := tt.AccessByRole(ui.RoleRadio, "热播榜"); ok {
		t.Fatal("the board of the tab before still shows")
	}
}

// An actor on a ranking opens their page over it, and its back button
// returns.
func TestActorOpensCollection(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{}, func(func()) {})
	app.Page = "rank"
	cs := app.Col("rank")
	cs.First = &source.CollectionPage{Layout: source.LayoutRanked}
	cs.Results.Loaded = true
	cs.Results.Items = []Card{{Kind: source.CardCollection, ID: "person:u1", Title: "李柯以", Shape: source.ShapeCircle, Note: "1060万人气"}}
	tt := ui.NewTester(func(c *ui.Context) {
		if n := len(app.Stack); n > 0 {
			// Its page shows once its data is in, so nothing is fetched.
			top := app.Stack[n-1]
			top.Results.Loaded = true
			top.First = &source.CollectionPage{}
			top.Header = &source.Header{Title: "李柯以", Meta: []source.Meta{{Value: "女演员"}}}
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
	if len(app.Stack) != 1 || app.Stack[0].ID != "person:u1" {
		t.Fatalf("stack = %+v", app.Stack)
	}
	accessOf(t, tt, ui.RoleHeading, "李柯以")
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.Stack) != 0 {
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
	app.OpenCollection(Card{Kind: source.CardCollection, ID: "person:u1", Title: "李柯以", Image: "x", Shape: source.ShapeCircle})
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
	cs := app.Stack[0]
	if cs.Header == nil || cs.Header.Description != "演员简介文本" || len(cs.Results.Items) != 1 {
		t.Fatalf("after retry: header %+v, items %+v", cs.Header, cs.Results.Items)
	}
	accessOf(t, tt, ui.RoleButton, "代表作")
}
