package app

import (
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
	"github.com/egoist/mygo/ui"

	"hongguo/internal/library"
)

// Times show as minutes and seconds, and hours only past an hour.
func TestFmtTime(t *testing.T) {
	for in, want := range map[float64]string{-3: "00:00", 0: "00:00", 59.6: "01:00", 145.4: "02:25", 3725: "1:02:05"} {
		if got := fmtTime(in); got != want {
			t.Errorf("fmtTime(%v) = %q, want %q", in, got, want)
		}
	}
}

// The grid fits as many cards as it can, never fewer than two, and fills
// the width exactly.
func TestColumns(t *testing.T) {
	for _, w := range []float32{200, 700, 1036, 1800} {
		n, cw := columns(w)
		if n < 2 || cw < cardMinW && n > 2 {
			t.Errorf("columns(%v) = %d of %v", w, n, cw)
		}
		if got := float32(n)*cw + float32(n-1)*cardGap; got < w-0.5 || got > w+0.5 {
			t.Errorf("columns(%v) fills %v", w, got)
		}
	}
}

// Choosing a genre clears the other rows, whose options depend on it;
// choosing the same option again changes nothing.
func TestFilterChoose(t *testing.T) {
	var fs filterState
	if !fs.choose("category_dim_theme", "cate_5000") || fs.shown != "category_dim_theme=cate_5000" {
		t.Fatalf("choose theme: %+v", fs.chosen)
	}
	fs.results.loaded = true
	if fs.choose("category_dim_theme", "cate_5000") || !fs.results.loaded {
		t.Error("choosing the same option again reset the results")
	}
	if !fs.choose("genre", "comic_series") || len(fs.chosen) != 1 || fs.shown != "genre=comic_series" {
		t.Errorf("choose genre: %v", fs.chosen)
	}
	if fs.results.loaded {
		t.Error("a new choice should drop the old results")
	}
	fs.choose("sort", "hot")
	if !fs.choose("sort", "") || len(fs.chosen) != 1 {
		t.Errorf("all: %v", fs.chosen)
	}
	fs.clear()
	if len(fs.chosen) != 0 || fs.shown != "" {
		t.Errorf("clear: %v", fs.chosen)
	}
}

// An actor on the ranking opens their page, and its back button returns.
func TestActorOpensPerson(t *testing.T) {
	app := &App{pal: lightPalette(), posters: newPosters(t.TempDir(), func() {}), lib: library.Load(t.TempDir()), page: pageRank}
	app.rank.page = &RankPage{
		Contents: []model.RankContent{{ID: "ranklist_celebrity", Name: "演员", Boards: []model.RankBoard{{ID: "b", Name: "剧星榜"}}}},
		Content:  "ranklist_celebrity", Board: "b",
	}
	app.rank.actors = []Actor{{ID: "u1", Name: "李柯以", Score: "1060万人气", Details: []string{"54部作品"}}}
	// The actor's page shows only once its data is in, so nothing is fetched.
	tt := ui.NewTester(func(c *ui.Context) {
		switch {
		case app.person == nil:
			app.content(c)
		case app.person.info != nil:
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
	// Loaded already, so the page asks for nothing.
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

// The player's buttons take their clicks: the picture, laid over the whole
// stage, once covered them and took every click as play/pause.
func TestStageButtonsTakeClicks(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: library.Load(t.TempDir())}
	w := &Watching{app: app, panel: true, index: 1,
		detail: &Detail{Episodes: make([]model.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	if err := tt.Click("选集面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.panel {
		t.Fatal("the panel button didn't take the click")
	}
	if w.paused {
		t.Fatal("the click went to the picture")
	}
}
