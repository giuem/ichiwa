package app

import (
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
	"github.com/egoist/mygo/ui"
)

// Choosing a genre clears the other rows, whose options depend on it;
// choosing the same option again changes nothing. A new choice drops the
// results and the loads under way.
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
	if !fs.choose("genre", "comic_series") || len(fs.chosen) != 1 || fs.chosen["genre"] != "comic_series" {
		t.Errorf("choose genre: %v", fs.chosen)
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

// A page of only actors doesn't end the ranking; a page with no new ones
// does.
func TestActorPagesKeepRanking(t *testing.T) {
	var r rankState
	// Each page as loadMore hands it over: having added no dramas, it took
	// the page for the end.
	load := func(ids ...string) {
		p := &RankPage{HasMore: true, Cursor: "c"}
		for _, id := range ids {
			p.Actors = append(p.Actors, Actor{ID: id})
		}
		r.list.more = false
		r.loaded(feedPage{cursor: "c", more: true, extra: p})
	}
	load("a", "b")
	if len(r.actors) != 2 || !r.list.more {
		t.Fatalf("first page: %d actors, more %v", len(r.actors), r.list.more)
	}
	load("b", "c")
	if len(r.actors) != 3 || !r.list.more {
		t.Fatalf("second page: %d actors, more %v", len(r.actors), r.list.more)
	}
	load("a", "c")
	if r.list.more {
		t.Fatal("a page of nothing new should end the ranking")
	}
}

// An actor on the ranking opens their page, and its back button returns.
func TestActorOpensPerson(t *testing.T) {
	app := &App{pal: lightPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t), page: pageRank}
	app.rank.page = &RankPage{
		Contents: []model.RankContent{{ID: "ranklist_celebrity", Name: "演员", Boards: []model.RankBoard{{ID: "b", Name: "剧星榜"}}}},
		Content:  "ranklist_celebrity", Board: "b",
	}
	// Loaded already, so the page asks for nothing.
	app.rank.list = Feed{fetch: func(string) (feedPage, error) { return feedPage{}, nil }, loaded: true}
	app.rank.actors = []Actor{{ID: "u1", Name: "李柯以", Score: "1060万人气", Details: []string{"54部作品"}}}
	// The actor's page shows only once its data is in, so nothing is fetched.
	tt := ui.NewTester(func(c *ui.Context) {
		if app.person == nil || app.person.info != nil {
			app.content(c)
		}
	}, 1100, 700)
	tt.Frame()
	if err := tt.Click("李柯以"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.person == nil || app.person.actor.ID != "u1" {
		t.Fatalf("person = %+v", app.person)
	}
	app.person.info = &Person{ID: "u1", Name: "李柯以", Followers: "458.6万", Tags: []string{"女演员"}}
	app.person.works = Feed{loaded: true, items: []Show{{ID: "s1", Title: "一江春水", Episodes: 81}}}
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
