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

// A header shows what it says on every page: an opened collection without
// images (a list of dramas) and a navigation page, whose description
// already shows as its subtitle.
func TestHeaderShowsWithoutImages(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{InfoData: source.Info{ID: "a"}}, func(func()) {})
	loaded := func(cs *Collection, h *source.Header) {
		cs.First, cs.Header = &source.CollectionPage{Header: h}, h
		cs.Results.Loaded, cs.Results.More = true, false
		cs.Results.Items = []Card{{Kind: source.CardMedia, ID: "m1", Title: "好剧"}}
	}
	app.OpenCollection(Card{Kind: source.CardCollection, ID: "list:1", Title: "片单"})
	loaded(app.Stack[0], &source.Header{Title: "夏日片单", Description: "片单的介绍", Meta: []source.Meta{{Value: "12部"}}})
	tt := ui.NewTester(app.content, 1100, 700)
	tt.Frame()
	accessOf(t, tt, ui.RoleHeading, "夏日片单")
	for _, s := range []string{"片单的介绍", "12部"} {
		if !tt.HasText(s) {
			t.Errorf("the opened page leaves out %q", s)
		}
	}

	app.CloseCollection()
	app.Page = "home"
	loaded(app.Col("home"), &source.Header{Description: "每天的好剧", Meta: []source.Meta{{Value: "每日更新"}}})
	tt.Frame()
	if !tt.HasText("每日更新") {
		t.Error("the navigation page leaves out its meta")
	}
	n := 0
	for _, s := range tt.Texts() {
		if s == "每天的好剧" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the description shows %d times", n)
	}
}

// What a card opens is up to its kind, round or not: a round drama plays,
// and a list where only some cards are round draws them all as cards.
func TestRoundCardsOpenByKind(t *testing.T) {
	src := &sourcetest.Source{InfoData: source.Info{ID: "a"}}
	for _, items := range [][]Card{
		{{Kind: source.CardMedia, ID: "m1", Title: "圆剧", Shape: source.ShapeCircle}},
		{{Kind: source.CardCollection, ID: "person:1", Title: "人", Shape: source.ShapeCircle}, {Kind: source.CardMedia, ID: "m1", Title: "圆剧"}},
	} {
		app := collectionApp(t, src, func(func()) {})
		app.Page = "home"
		cs := app.Col("home")
		cs.First = &source.CollectionPage{}
		cs.Results.Loaded, cs.Results.More, cs.Results.Items = true, false, items
		tt := ui.NewTester(app.content, 1100, 700)
		tt.Frame()
		if err := tt.Click("圆剧"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if app.Watch == nil || len(app.Stack) != 0 {
			t.Errorf("%d cards: the drama opened a collection, not its player", len(items))
		}
		app.CloseWatch()
	}
}

// A page with filter rows can still load anew, beside going back to no
// filters.
func TestFilterPageRefreshes(t *testing.T) {
	app := collectionApp(t, &sourcetest.Source{InfoData: source.Info{ID: "a"}}, func(func()) {})
	app.Page = "filter"
	cs := app.Col("filter")
	cs.First = &source.CollectionPage{}
	cs.Rows = []FilterRow{{Key: "genre", Title: "体裁", Value: "comic", Options: []source.FilterOption{{Title: "全部体裁"}, {Value: "comic", Title: "漫剧"}}}}
	cs.Chosen = map[string]string{"genre": "comic"}
	cs.Results.Loaded, cs.Results.More = true, false
	cs.Results.Items = []Card{{Kind: source.CardMedia, ID: "m1", Title: "好剧"}}
	tt := ui.NewTester(app.content, 1100, 700)
	tt.Frame()
	accessOf(t, tt, ui.RoleButton, "重置")
	if err := tt.Click("刷新"); err != nil {
		t.Fatal(err)
	}
	if cs.Results.Loaded || cs.Chosen["genre"] != "comic" {
		t.Errorf("refresh: loaded %v, chosen %v", cs.Results.Loaded, cs.Chosen)
	}
}
