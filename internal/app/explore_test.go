package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
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

// Actor ranking pages live in the feed directly; new actors keep the ranking
// going and repeated pages eventually end it.
func TestActorPagesKeepRanking(t *testing.T) {
	var r rankState
	load := func(ids ...string) {
		var items []Card
		for _, id := range ids {
			items = append(items, Card{Kind: source.CardCollection, ID: "person:" + id, Shape: source.ShapeCircle})
		}
		fp := feedPage{items: items, cursor: "c", more: true}
		fresh, _ := r.list.take(fp)
		r.list.more = r.list.step(fp, fresh)
	}
	load("a", "b")
	if len(r.list.items) != 2 || !r.list.more {
		t.Fatalf("first page: %d actors, more %v", len(r.list.items), r.list.more)
	}
	load("b", "c")
	if len(r.list.items) != 3 || !r.list.more {
		t.Fatalf("second page: %d actors, more %v", len(r.list.items), r.list.more)
	}
	for range maxStalls {
		load("a", "c")
	}
	if r.list.more {
		t.Fatal("repeated pages of nothing new should end the ranking")
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
