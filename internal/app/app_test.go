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

// While more pages come, the grid shows whole rows only; the last page
// shows what is left, and so does a feed shorter than a row.
func TestVisibleWholeRows(t *testing.T) {
	f := &Feed{more: true}
	for i := range 13 {
		f.add([]Show{{ID: itoa(i)}})
	}
	if got := len(visible(f, 5)); got != 10 {
		t.Errorf("more: shows %d, want 10", got)
	}
	f.err = "boom"
	if got := len(visible(f, 5)); got != 13 {
		t.Errorf("failed: shows %d, want 13", got)
	}
	f.err, f.more = "", false
	if got := len(visible(f, 5)); got != 13 {
		t.Errorf("ended: shows %d, want 13", got)
	}
	short := &Feed{more: true, items: make([]Show, 3)}
	if got := len(visible(short, 5)); got != 3 {
		t.Errorf("short: shows %d, want 3", got)
	}
}

// A reset feed keeps its source, starts over, and tells loads under way
// apart from new ones.
func TestFeedReset(t *testing.T) {
	f := newFeed(func(string) (feedPage, error) { return feedPage{}, nil })
	f.add([]Show{{ID: "a"}})
	f.cursor, f.more, f.loading = "c", false, true
	f.reset()
	if f.fetch == nil || !f.more || f.loading || len(f.items) != 0 || f.cursor != "" || f.gen != 1 {
		t.Fatalf("reset = %+v", f)
	}
	f.add([]Show{{ID: "a"}})
	if len(f.items) != 1 {
		t.Fatal("seen survived the reset")
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

// Pinning the window folds the panel, and unpinning opens it again unless
// the user opened or closed it meanwhile.
func TestPinFoldsPanel(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: library.Load(t.TempDir())}
	w := &Watching{app: app, panel: true, index: 1,
		detail: &Detail{Episodes: make([]model.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	pin := func() {
		t.Helper()
		if err := tt.Click("窗口置顶 (T)"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	pin()
	if !app.lib.OnTop || w.panel {
		t.Fatalf("pinned: on top %v, panel %v", app.lib.OnTop, w.panel)
	}
	if w.paused {
		t.Fatal("the click went to the picture")
	}
	pin()
	if app.lib.OnTop || !w.panel {
		t.Fatalf("unpinned: on top %v, panel %v", app.lib.OnTop, w.panel)
	}
	pin()
	if err := tt.Click("选集面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("选集面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	pin()
	if w.panel {
		t.Fatal("unpinning opened a panel the user closed")
	}
}

// seasonsWatching is the last episode of the first of two seasons, noting
// the season it switches to.
func seasonsWatching(t *testing.T, secondOnline bool) (*Watching, *[]Season, *[]int) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: library.Load(t.TempDir())}
	w := &Watching{app: app, panel: true, index: 2, show: Show{ID: "s1"},
		detail: &Detail{Episodes: make([]model.Episode, 3)},
		seasons: []Season{
			{Show: Show{ID: "s1", Title: "剧 第1季"}, Index: 1, Online: true},
			{Show: Show{ID: "s2", Title: "剧 第2季"}, Index: 2, Online: secondOnline, Note: "预计11月10日上线"},
		}}
	var to []Season
	var eps []int
	w.switchSeason = func(s Season, ep int) { to, eps = append(to, s), append(eps, ep) }
	return w, &to, &eps
}

// The last episode of a season goes on to the next season's first, when
// it is out.
func TestEndedGoesToNextSeason(t *testing.T) {
	w, to, eps := seasonsWatching(t, true)
	w.onEnded()
	if len(*to) != 1 || (*to)[0].ID != "s2" || (*eps)[0] != 0 {
		t.Fatalf("switched to %v at %v", *to, *eps)
	}
	if w.ended {
		t.Error("ended although the next season is out")
	}
	w, to, _ = seasonsWatching(t, true)
	w.step(1)
	if len(*to) != 1 {
		t.Error("the next button didn't go to the next season")
	}
}

// A season not out yet is not switched to; the page ends and says when it
// comes out.
func TestEndedWaitsForAnnouncedSeason(t *testing.T) {
	w, to, _ := seasonsWatching(t, false)
	w.onEnded()
	if len(*to) != 0 || !w.ended {
		t.Fatalf("switched to %v, ended %v", *to, w.ended)
	}
	if got := seasonLine(w.seasons[1]); got != "第2季 · 预计11月10日上线" {
		t.Errorf("seasonLine = %q", got)
	}
}

// The season row switches to a season where the user left it.
func TestSeasonRowSwitches(t *testing.T) {
	w, to, eps := seasonsWatching(t, true)
	tt := ui.NewTester(func(c *ui.Context) { w.app.seasonRow(c, w) }, 368, 200)
	tt.Frame()
	if err := tt.Click("第2季"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*to) != 1 || (*to)[0].ID != "s2" || (*eps)[0] != -1 {
		t.Fatalf("switched to %v at %v", *to, *eps)
	}
}

func TestWantsDisplay(t *testing.T) {
	app := &App{}
	playing := new(ui.VideoPlayer)
	for _, c := range []struct {
		name string
		w    Watching
		want bool
	}{
		{"loading the next episode", Watching{loading: true}, true},
		{"playing", Watching{player: playing}, true},
		{"paused", Watching{player: playing, paused: true}, false},
		{"ended", Watching{player: playing, ended: true}, false},
		{"failed", Watching{err: "播放失败"}, false},
		{"nothing to play", Watching{}, false},
	} {
		w := c.w
		w.app, app.watch = app, &w
		if got := w.wantsDisplay(); got != c.want {
			t.Errorf("%s: wantsDisplay = %v, want %v", c.name, got, c.want)
		}
	}
	w := Watching{app: app, loading: true}
	app.watch = nil
	if w.wantsDisplay() {
		t.Error("a page left still wants the display")
	}
}
