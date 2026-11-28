package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	core "github.com/giuem/ichiwa/internal/app"
	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// Times show as minutes and Seconds, and hours only past an hour.
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

// The player's buttons take their clicks: the picture, laid over the whole
// stage, once covered them and took every click as play/pause.
func TestStageButtonsTakeClicks(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, Panel: true, Index: 1,
		Detail: &Media{Episodes: make([]source.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	if err := tt.Click("选集面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.Panel {
		t.Fatal("the panel button didn't take the click")
	}
	if w.Paused {
		t.Fatal("the click went to the picture")
	}
}

// Pinning the window folds the panel, and unpinning opens it again unless
// the user opened or closed it meanwhile.
func TestPinFoldsPanel(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, Panel: true, Index: 1,
		Detail: &Media{Episodes: make([]source.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	pin := func() {
		t.Helper()
		if err := tt.Click("窗口置顶"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	pin()
	if !app.Lib.OnTop || w.Panel {
		t.Fatalf("pinned: on top %v, panel %v", app.Lib.OnTop, w.Panel)
	}
	if w.Paused {
		t.Fatal("the click went to the picture")
	}
	pin()
	if app.Lib.OnTop || !w.Panel {
		t.Fatalf("unpinned: on top %v, panel %v", app.Lib.OnTop, w.Panel)
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
	if w.Panel {
		t.Fatal("unpinning opened a panel the user closed")
	}
}

// seasonsWatching is the last episode of the first of two seasons, noting
// the season it switches to.
func seasonsWatching(t *testing.T, secondOnline bool) (*Watching, *[]Season, *[]int) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, SourceID: "hongguo", Panel: true, Index: 2, Show: Card{Kind: source.CardMedia, ID: "s1"},
		Detail: &Media{Episodes: make([]source.Episode, 3)},
		Seasons: []Season{
			{ID: "s1", Title: "第1季", Current: true, Available: source.Bool(true)},
			{ID: "s2", Title: "第2季", Available: source.Bool(secondOnline), Note: "预计11月10日上线"},
		}}
	var to []Season
	var eps []int
	w.SwitchSeason = func(s Season, ep int) { to, eps = append(to, s), append(eps, ep) }
	return w, &to, &eps
}

// The season row switches to a season where the user left it.
func TestSeasonRowSwitches(t *testing.T) {
	w, to, eps := seasonsWatching(t, true)
	w.Seasons[1].Note = ""
	v := &App{App: w.App, pal: darkPalette(), posters: newPosters(t.TempDir(), func() {})}
	tt := ui.NewTester(func(c *ui.Context) { v.seasonRow(c, w) }, 368, 200)
	tt.Frame()
	if err := tt.Click("第2季"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*to) != 1 || (*to)[0].ID != "s2" || (*eps)[0] != -1 {
		t.Fatalf("switched to %v at %v", *to, *eps)
	}
}

// In full screen the controls go once the pointer rests, even over the
// video; in a window they also need the pointer over it.
func TestChromeShown(t *testing.T) {
	for _, tt := range []struct {
		playing, hover, full bool
		still                time.Duration
		want                 bool
	}{
		{playing: false, hover: false, full: true, still: time.Hour, want: true},
		{playing: true, hover: true, full: true, still: chromeLinger - 1, want: true},
		{playing: true, hover: true, full: true, still: chromeLinger, want: false},
		{playing: true, hover: true, full: false, still: chromeLinger - 1, want: true},
		{playing: true, hover: true, full: false, still: chromeLinger, want: false},
		{playing: true, hover: false, full: false, want: false},
	} {
		if got := chromeShown(tt.playing, tt.hover, tt.full, tt.still); got != tt.want {
			t.Errorf("chromeShown(%+v) = %v", tt, got)
		}
	}
}

// The episodes open over the picture in full screen, and a click beside
// them closes them without reaching the picture.
func TestDrawerClosesOnClickBeside(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, Index: 1, Detail: &Media{Episodes: []source.Episode{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, Drawer: true}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, true) }, 1200, 700)
	tt.Frame()
	tt.ClickAt(100, 300)
	tt.Frame()
	if w.Drawer {
		t.Fatal("still open")
	}
	if !w.ClickAt.IsZero() || w.Paused {
		t.Fatal("the click reached the picture")
	}
}

// The picture takes the clicks that land on it, and what shows over it
// still takes its own: nothing in the flow may cover them.
func TestPictureTakesClicks(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, Index: 1, Detail: &Media{Episodes: []source.Episode{{ID: "a"}, {ID: "b"}, {ID: "c"}}}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	tt.ClickAt(200, 300)
	tt.Frame()
	if w.ClickAt.IsZero() {
		t.Fatal("the picture got no click")
	}
	w.ClickAt, w.Halt = time.Time{}, true
	tt.Frame()
	if err := tt.Click("重播"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.Halt {
		t.Fatal("the button over the picture got no click")
	}
}

// The controls of the narrowest stage still build, for the small window.
func TestStageBuildsNarrow(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, Index: 1, Detail: &Media{Episodes: make([]source.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, core.MiniMinWidth, core.MiniMinHeight)
	tt.Frame()
	tt.Frame()
}

// The seek bar builds and takes the pointer with no player yet.
func TestProgressWithoutPlayer(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App}
	tt := ui.NewTester(func(c *ui.Context) { app.progress(c, w) }, 400, 80)
	tt.Frame()
	tt.Move(100, 10)
	tt.Frame()
	tt.Frame()
}

// A button clicked in a KeepFocus root leaves Space to the shortcut, once
// and only once; Tab still gives the button the key.
func TestKeepFocusLeavesSpaceToShortcut(t *testing.T) {
	var clicks, spaces int
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().KeepFocus()
		c.OnShortcut(0, ui.KeySpace, func() { spaces++ })
		if ui.ButtonBase(c).Size(80, 40).Label("go").Clicked() {
			clicks++
		}
	}, 200, 200)
	if err := tt.Click("go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if clicks != 1 || spaces != 1 {
		t.Fatalf("after a click and Space: %d clicks, %d shortcuts", clicks, spaces)
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if clicks != 2 || spaces != 1 {
		t.Fatalf("after Tab and Space: %d clicks, %d shortcuts", clicks, spaces)
	}
}

// testLibrary closes its database before the test's directory goes, which
// Windows refuses while the file is open.
func testLibrary(t *testing.T) *library.Library {
	lib := library.Load(t.TempDir())
	t.Cleanup(lib.Close)
	return lib
}

// The speed button opens a list of the speeds; choosing one sets it and
// closes the list.
func TestSpeedPicker(t *testing.T) {
	app := viewApp(t, darkPalette())
	app.Lib.Speed = 1
	w := &Watching{App: app.App, Index: 1, Detail: &Media{Episodes: make([]source.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	if err := tt.Click("播放速度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !uiOf(w).speedOpen || app.Lib.Speed != 1 {
		t.Fatalf("clicked the button: open %v, speed %v", uiOf(w).speedOpen, app.Lib.Speed)
	}
	if a, ok := tt.AccessByRole(ui.RoleOption, "1倍"); !ok || !a.Selected {
		t.Fatalf("the current speed isn't selected: %+v", a)
	}
	if err := tt.Click("1.5倍"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if uiOf(w).speedOpen || app.Lib.Speed != 1.5 {
		t.Fatalf("chose 1.5倍: open %v, speed %v", uiOf(w).speedOpen, app.Lib.Speed)
	}
}

// Sidebar search and player danmaku controls follow source capabilities.
func TestCapabilityHiding(t *testing.T) {
	noCaps := &sourcetest.Source{InfoData: source.Info{ID: "plain", Name: "Plain"}}
	app := viewApp(t, darkPalette(), noCaps)
	app.Nav = []source.NavItem{{ID: "home", Title: "首页", Icon: source.IconHome}}
	tt := ui.NewTester(app.sidebar, 220, 600)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "搜索"); ok {
		t.Error("search nav item shown without CapSearch")
	}

	w := &Watching{App: app.App, Src: noCaps, Index: 0, Detail: &Media{Episodes: []source.Episode{{ID: "e1"}}}}
	ttStage := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	ttStage.Frame()
	if _, ok := ttStage.AccessByRole(ui.RoleToggleButton, "弹幕"); ok {
		t.Error("danmaku toggle shown without CapDanmaku")
	}

	withCaps := &sourcetest.Source{InfoData: source.Info{
		ID:           "full",
		Name:         "Full",
		Capabilities: []source.Capability{source.CapSearch, source.CapDanmaku},
	}}
	app.Src, w.Src = withCaps, withCaps
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "搜索"); !ok {
		t.Error("search nav item hidden with CapSearch")
	}
	ttStage.Frame()
	if _, ok := ttStage.AccessByRole(ui.RoleToggleButton, "弹幕"); !ok {
		t.Error("danmaku toggle hidden with CapDanmaku")
	}
}

func TestSeasonCardUsesRealNameAndImage(t *testing.T) {
	named := Season{
		ID:        "s2",
		Title:     "第2季",
		Name:      "某剧 第二季",
		Image:     "https://cdn.example.com/s2.jpg",
		Available: source.Bool(false),
	}
	c := core.SeasonCard(named)
	if c.ID != "s2" || c.Title != "某剧 第二季" || c.Image != "https://cdn.example.com/s2.jpg" || c.Kind != source.CardMedia {
		t.Fatalf("seasonCard(named) = %+v", c)
	}

	// Omitting Name falls back to Season.Title, never concatenating the current drama title.
	unnamed := Season{ID: "s3", Title: "第3季", Available: source.Bool(false)}
	c2 := core.SeasonCard(unnamed)
	if c2.Title != "第3季" || c2.Image != "" {
		t.Fatalf("seasonCard(unnamed) = %+v", c2)
	}

	// Following an announced season from the ended overlay stores its real name and poster.
	w, _, _ := seasonsWatching(t, false)
	w.Show = Card{Kind: source.CardMedia, ID: "s1", Title: "某剧 第一季", Image: "https://cdn.example.com/s1.jpg"}
	w.Seasons[1].Name = "某剧 第二季"
	w.Seasons[1].Image = "https://cdn.example.com/s2.jpg"
	w.Ended = true
	v := &App{App: w.App, pal: darkPalette(), posters: newPosters(t.TempDir(), func() {})}
	tt := ui.NewTester(func(c *ui.Context) { v.stageMessage(c, w) }, 800, 500)
	tt.Frame()
	if err := tt.Click("追第2季"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	followed := w.App.Lib.Followed()
	if len(followed) != 1 || followed[0].Card.Title != "某剧 第二季" || followed[0].Card.Image != "https://cdn.example.com/s2.jpg" {
		t.Fatalf("followed announced season = %+v", followed)
	}
}

// The cast row shows each person's name and role under their avatar.
func TestCastNamesUnderAvatars(t *testing.T) {
	app := viewApp(t, darkPalette())
	w := &Watching{App: app.App, SourceID: "hongguo", Show: Card{Kind: source.CardMedia, ID: "s1"},
		Detail: &Media{People: []Card{{Kind: source.CardCollection, ID: "person:1", Title: "张三", Subtitle: "饰 李四", Shape: source.ShapeCircle}}}}
	tt := ui.NewTester(func(c *ui.Context) { app.related(c, w) }, 368, 300)
	tt.Frame()
	b := accessOf(t, tt, ui.RoleButton, "张三").Bounds
	if b.H < 52+2*12 {
		t.Errorf("the button is %v tall: no room under the avatar", b.H)
	}
	r, ok := tt.Find("饰 李四")
	if !ok || r.W < 30 || r.Y < b.Y+52 || r.X < b.X || r.X+r.W > b.X+b.W+0.5 {
		t.Errorf("the role at %+v, not under the avatar in %+v", r, b)
	}
}
