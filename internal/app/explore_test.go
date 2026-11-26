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
// same option again changes nothing; applyPage syncs dependent rows returned
// by the source.
func TestFilterChoose(t *testing.T) {
	var fs filterState
	fs.results.fetch = func(string) (feedPage, error) { return feedPage{}, nil }
	if !fs.choose("category_dim_theme", "cate_5000") || fs.chosen["category_dim_theme"] != "cate_5000" {
		t.Fatalf("choose theme: %+v", fs.chosen)
	}
	if fs.results.fetch != nil || fs.results.gen != 1 {
		t.Error("a new choice should drop the old results and their fetch")
	}
	fs.results.loaded = true
	if fs.choose("category_dim_theme", "cate_5000") || !fs.results.loaded {
		t.Error("choosing the same option again reset the results")
	}
	if !fs.choose("genre", "comic_series") || fs.chosen["genre"] != "comic_series" {
		t.Errorf("choose genre: %v", fs.chosen)
	}
	// Source returns updated filter rows clearing incompatible selections.
	fs.applyPage(feedPage{page: &source.CollectionPage{
		Filters: []FilterRow{
			{Key: "genre", Title: "全部体裁", Value: "comic_series", Options: []source.FilterOption{{Value: "", Title: "全部体裁"}, {Value: "comic_series", Title: "漫剧"}}},
			{Key: "category_dim_theme", Title: "全部题材", Value: "", Options: []source.FilterOption{{Value: "", Title: "全部题材"}}},
		},
	}})
	if len(fs.chosen) != 1 || fs.chosen["genre"] != "comic_series" {
		t.Errorf("applyPage sync: %v", fs.chosen)
	}
	fs.choose("sort", "hot")
	if !fs.choose("sort", "") || len(fs.chosen) != 1 {
		t.Errorf("all: %v", fs.chosen)
	}
	fs.clear()
	if len(fs.chosen) != 0 {
		t.Errorf("clear: %v", fs.chosen)
	}
}

// First load where the filter panel succeeds and results fail still displays
// the filter panel and lets the user retry the results.
func TestFilterPageFirstLoadFailureShowsPanel(t *testing.T) {
	done := make(chan func(), 8)
	var calls atomic.Int32
	panel := []FilterRow{{
		Key:   "genre",
		Title: "全部体裁",
		Options: []source.FilterOption{
			{Value: "", Title: "全部体裁"},
			{Value: "comic_series", Title: "漫剧"},
		},
	}}
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "hongguo"},
		OnCollection: func(_ context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if calls.Add(1) == 1 {
				return &source.CollectionPage{
					Title:   "筛选",
					Layout:  source.LayoutGrid,
					Filters: panel,
				}, source.NewError(source.ErrNetwork, "网络连接失败", nil)
			}
			return &source.CollectionPage{
				Title:   "筛选",
				Layout:  source.LayoutGrid,
				Filters: panel,
				Items:   []Card{{Kind: source.CardMedia, ID: "m1", Title: "好剧"}},
			}, nil
		},
	}
	app := &App{
		src:     src,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		page:    pageFilter,
		warmed:  true,
		post:    func(fn func()) { done <- fn },
	}
	drain := func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}

	tt := ui.NewTester(app.filterPage, 1100, 700)
	tt.Frame()
	drain()
	tt.Frame()

	if !app.filter.rowsOK {
		t.Fatal("filter panel should be shown even when results query failed")
	}
	if _, ok := tt.AccessByRole(ui.RoleRadio, "漫剧"); !ok {
		t.Fatal("filter option 漫剧 missing after first-load results failure")
	}
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("retry button missing: %v", err)
	}
	tt.Frame()
	drain()
	tt.Frame()

	if app.filter.results.err != "" || len(app.filter.results.items) != 1 {
		t.Fatalf("after retry: err=%q items=%+v", app.filter.results.err, app.filter.results.items)
	}
}

// Actor ranking pages live in the feed directly; new actors keep the ranking
// going, and the final page (no cursor) or a repeat-only page ends immediately
// without extra stall retries.
func TestActorPagesKeepRanking(t *testing.T) {
	var r rankState
	load := func(cursor string, more bool, ids ...string) {
		var items []Card
		for _, id := range ids {
			items = append(items, Card{Kind: source.CardCollection, ID: "person:" + id, Shape: source.ShapeCircle})
		}
		fp := feedPage{items: items, cursor: cursor, more: more}
		fresh, _ := r.list.take(fp)
		r.list.more = r.list.step(fp, fresh)
		r.loaded(fp)
	}
	load("c1", true, "a", "b")
	if len(r.list.items) != 2 || !r.list.more {
		t.Fatalf("first page: %d actors, more %v", len(r.list.items), r.list.more)
	}
	load("c2", true, "b", "c")
	if len(r.list.items) != 3 || !r.list.more {
		t.Fatalf("second page: %d actors, more %v", len(r.list.items), r.list.more)
	}
	// Last page (no next cursor) ends immediately in one step.
	load("", false, "d")
	if len(r.list.items) != 4 || r.list.more {
		t.Fatalf("last page should end immediately: len=%d more=%v", len(r.list.items), r.list.more)
	}

	// A repeat-only page also ends immediately.
	r = rankState{}
	load("c1", true, "a", "b")
	load("c2", true, "a", "b")
	if r.list.more {
		t.Fatal("repeat-only actor page should end immediately")
	}
}

// Switching the ranking category tab immediately hides the previous category's
// sub-boards before the new page returns.
func TestRankSwitchCategoryClearsSubBoards(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "hongguo"},
		OnCollection: func(context.Context, source.CollectionRequest) (*source.CollectionPage, error) {
			<-block
			return &source.CollectionPage{}, nil
		},
	}
	app := &App{
		src:     src,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		page:    pageRank,
		warmed:  true,
		post:    func(func()) {},
	}
	app.rank.filters = []FilterRow{
		{
			Key: "content", Title: "榜单类别", Value: "human",
			Options: []source.FilterOption{{Value: "human", Title: "真人剧"}, {Value: "actors", Title: "演员"}},
		},
		{
			Key: "board", Title: "榜单", Value: "hot",
			Options: []source.FilterOption{{Value: "hot", Title: "热播榜"}, {Value: "new", Title: "新剧榜"}},
		},
	}
	app.rank.chosen = map[string]string{"content": "human", "board": "hot"}
	app.rank.list = Feed{
		fetch:  func(string) (feedPage, error) { return feedPage{}, nil },
		loaded: true,
		items:  []Card{{Kind: source.CardMedia, ID: "m1", Title: "热播剧"}},
	}

	tt := ui.NewTester(app.rankPage, 1100, 700)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleRadio, "热播榜"); !ok {
		t.Fatal("expected initial sub-board 热播榜")
	}
	if err := tt.Click("演员"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.rank.filters) != 1 {
		t.Fatalf("filters not truncated on category switch: %+v", app.rank.filters)
	}
	if _, ok := tt.AccessByRole(ui.RoleRadio, "热播榜"); ok {
		t.Fatal("previous category sub-board 热播榜 still shown after switching category")
	}
}

// A ranking that returns items without filter rows renders its items instead of
// staying stuck on a loading skeleton.
func TestRankWithoutFiltersShowsItems(t *testing.T) {
	app := &App{
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		page:    pageRank,
	}
	app.rank.list = Feed{
		fetch:  func(string) (feedPage, error) { return feedPage{}, nil },
		loaded: true,
		items:  []Card{{Kind: source.CardMedia, ID: "m1", Title: "无筛选排行剧"}},
	}
	tt := ui.NewTester(app.rankPage, 1100, 700)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "无筛选排行剧"); !ok {
		t.Fatal("ranking item hidden when filters is empty")
	}
}

// Person header loads independently of works, and both header failures and
// works first-page failures show retry buttons that recover.
func TestPersonPageIndependentHeaderAndRetries(t *testing.T) {
	done := make(chan func(), 8)
	var failHeader atomic.Bool
	var failWorks atomic.Bool
	failHeader.Store(true)
	failWorks.Store(true)
	worksGate := make(chan struct{})

	src := &sourcetest.Source{
		InfoData: source.Info{ID: "hongguo"},
		OnCollection: func(_ context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			switch r.ID {
			case "person:u1":
				if failHeader.Load() {
					return nil, source.NewError(source.ErrNetwork, "简介加载失败", nil)
				}
				return &source.CollectionPage{
					Title: "李柯以",
					Header: &source.Header{
						Title:       "李柯以",
						Shape:       source.ShapeCircle,
						Description: "演员简介文本",
						Meta:        []source.Meta{{Value: "女演员"}},
					},
					Sections: []source.Section{{
						Title:      "作品",
						Layout:     source.SectionGrid,
						Collection: "person-works:u1",
					}},
				}, nil
			case "person-works:u1":
				<-worksGate
				if failWorks.Load() {
					return nil, source.NewError(source.ErrNetwork, "作品加载失败", nil)
				}
				return &source.CollectionPage{
					Title:  "作品",
					Layout: source.LayoutGrid,
					Items:  []Card{{Kind: source.CardMedia, ID: "s1", Title: "代表作"}},
				}, nil
			default:
				return nil, source.NewError(source.ErrNotFound, "不存在", nil)
			}
		},
	}

	app := &App{
		src:     src,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		warmed:  true,
		post:    func(fn func()) { done <- fn },
	}
	drain := func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}

	app.openPerson(Card{Kind: source.CardCollection, ID: "person:u1", Title: "李柯以"})
	tt := ui.NewTester(app.personPage, 1100, 700)

	// 1. Header fails first -> shows error + retry button.
	tt.Frame()
	drain()
	tt.Frame()
	if app.person.headerErr == "" {
		t.Fatal("expected header error")
	}

	// 2. Retry header while works is still blocked on worksGate -> header
	// appears immediately without waiting for works!
	failHeader.Store(false)
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("header retry button missing: %v", err)
	}
	tt.Frame()
	drain()
	tt.Frame()
	if app.person.header == nil || app.person.header.Description != "演员简介文本" {
		t.Fatalf("header did not load independently of works: %+v", app.person.header)
	}

	// 3. Unblock works with first-page failure -> shows works error + retry (not "暂无作品").
	close(worksGate)
	drain()
	tt.Frame()
	if app.person.works.err == "" {
		t.Fatal("expected works first-page error")
	}

	// 4. Retry works -> loads works grid.
	failWorks.Store(false)
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("works retry button missing: %v", err)
	}
	tt.Frame()
	drain()
	tt.Frame()
	if app.person.works.err != "" || len(app.person.works.items) != 1 || app.person.works.items[0].Title != "代表作" {
		t.Fatalf("works after retry: err=%q items=%+v", app.person.works.err, app.person.works.items)
	}
}

// An actor on the ranking opens their page, and its back button returns.
func TestActorOpensPerson(t *testing.T) {
	app := &App{pal: lightPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t), page: pageRank}
	app.rank.filters = []FilterRow{
		{Key: "content", Title: "榜单类别", Value: "ranklist_celebrity", Options: []source.FilterOption{{Value: "ranklist_celebrity", Title: "演员"}}},
		{Key: "board", Title: "榜单", Value: "b", Options: []source.FilterOption{{Value: "b", Title: "剧星榜"}}},
	}
	// Loaded already, so the page asks for nothing.
	app.rank.list = Feed{
		fetch:  func(string) (feedPage, error) { return feedPage{}, nil },
		loaded: true,
		items: []Card{{
			Kind:     source.CardCollection,
			ID:       "person:u1",
			Title:    "李柯以",
			Shape:    source.ShapeCircle,
			Note:     "1060万人气",
			Subtitle: "54部作品",
		}},
	}
	// The actor's page shows only once its data is in, so nothing is fetched.
	tt := ui.NewTester(func(c *ui.Context) {
		if app.person == nil || app.person.header != nil {
			app.content(c)
		}
	}, 1100, 700)
	tt.Frame()
	if err := tt.Click("李柯以"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.person == nil || app.person.actor.ID != "person:u1" {
		t.Fatalf("person = %+v", app.person)
	}
	app.person.header = &source.Header{
		Title: "李柯以",
		Shape: source.ShapeCircle,
		Meta:  []source.Meta{{Value: "女演员"}, {Label: "粉丝", Value: "458.6万"}},
	}
	app.person.works = Feed{
		loaded: true,
		items:  []Card{{Kind: source.CardMedia, ID: "s1", Title: "一江春水", Badges: []string{"全81集"}}},
	}
	tt.Frame()
	tt.Frame()
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.person != nil {
		t.Fatal("back didn't close the actor's page")
	}
}
