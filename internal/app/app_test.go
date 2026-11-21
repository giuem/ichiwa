package app

import (
	"testing"
	"time"

	"github.com/beilunyang/openhongguo/sdk/model"
	"github.com/egoist/mygo/ui"

	"ichiwa/internal/library"
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
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
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
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	w := &Watching{app: app, panel: true, index: 1,
		detail: &Detail{Episodes: make([]model.Episode, 3)}}
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
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
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

// Muting remembers the volume instead of coming back at full.
func TestMuteRestoresVolume(t *testing.T) {
	app := &App{lib: testLibrary(t)}
	app.lib.Volume = 0.4
	w := &Watching{app: app}
	w.toggleMute()
	if app.lib.Volume != 0 {
		t.Fatalf("muted at %v", app.lib.Volume)
	}
	w.toggleMute()
	if app.lib.Volume != 0.4 {
		t.Fatalf("unmuted at %v", app.lib.Volume)
	}
}

// A single click counts only after the wait; a double click goes to full
// screen and never touches the playback.
func TestPictureClicks(t *testing.T) {
	now := time.Now()
	at := func(ms int) time.Time { return now.Add(time.Duration(ms) * time.Millisecond) }
	w := &Watching{}
	if toggle, _ := w.pictureClicks(true, false, false, at(0)); toggle {
		t.Fatal("toggled before the wait")
	}
	if toggle, _ := w.pictureClicks(false, false, false, at(300)); !toggle {
		t.Fatal("the single click never counted")
	}

	w = &Watching{}
	w.pictureClicks(true, false, false, at(0))
	if toggle, full := w.pictureClicks(false, true, true, at(100)); toggle || !full {
		t.Fatalf("double click: toggle %v, full %v", toggle, full)
	}
	if toggle, _ := w.pictureClicks(true, false, false, at(150)); toggle {
		t.Fatal("the release of the double click toggled")
	}
	if toggle, _ := w.pictureClicks(false, false, false, at(900)); toggle {
		t.Fatal("a click was left waiting")
	}

	// Press and release of the second click in one frame.
	w = &Watching{}
	w.pictureClicks(true, false, false, at(0))
	if toggle, full := w.pictureClicks(true, true, false, at(100)); toggle || !full {
		t.Fatalf("one frame: toggle %v, full %v", toggle, full)
	}
	if w.skipClick {
		t.Fatal("the next click would be lost")
	}

	// Slower than the wait: the pause that happened is put back.
	w = &Watching{}
	w.pictureClicks(true, false, false, at(0))
	w.pictureClicks(false, false, false, at(250))
	if toggle, full := w.pictureClicks(false, true, true, at(450)); !toggle || !full {
		t.Fatalf("slow double click: toggle %v, full %v", toggle, full)
	}
}

// With autoplay off the page waits at the end of an episode.
func TestEndedHaltsWithoutAutoplay(t *testing.T) {
	w, to, _ := seasonsWatching(t, true)
	w.app.lib.Autoplay = false
	w.onEnded()
	if !w.halt || w.ended || len(*to) != 0 {
		t.Fatalf("halt %v, ended %v, switched %v", w.halt, w.ended, *to)
	}
	if w.wantsDisplay() {
		t.Error("keeps the display on while waiting")
	}
}

// Stopping at the end of the episode happens once.
func TestSleepEpisodeStopsOnce(t *testing.T) {
	w, _, _ := seasonsWatching(t, true)
	w.app.setSleep(sleepEpisode)
	w.onEnded()
	if !w.halt || w.app.sleepMins != 0 {
		t.Fatalf("halt %v, sleep %d", w.halt, w.app.sleepMins)
	}
}

func TestSleepTickWaitsForLoading(t *testing.T) {
	app := &App{lib: testLibrary(t)}
	w := &Watching{app: app, loading: true}
	app.setSleep(15)
	if w.sleepTick(time.Now().Add(time.Hour)) || app.sleepMins != 15 {
		t.Fatal("ran out during loading")
	}
	w.loading = false
	if w.sleepTick(time.Now()) {
		t.Fatal("ran out early")
	}
	if !w.sleepTick(time.Now().Add(time.Hour)) || app.sleepMins != 0 {
		t.Fatal("did not run out")
	}
}

func TestMiniSize(t *testing.T) {
	for _, tt := range []struct{ vw, vh, w, h int }{
		{1080, 1920, 360, 640}, {0, 0, 360, 640}, {1920, 1080, 640, 360}, {720, 2400, miniMinWidth, 640}, {2400, 720, 640, miniMinHeight},
	} {
		if w, h := miniSize(tt.vw, tt.vh); w != tt.w || h != tt.h {
			t.Errorf("miniSize(%d, %d) = %d, %d", tt.vw, tt.vh, w, h)
		}
	}
}

// The episodes open over the picture in full screen, and a click beside
// them closes them without reaching the picture.
func TestDrawerClosesOnClickBeside(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	w := &Watching{app: app, index: 1, detail: &Detail{Episodes: []model.Episode{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, drawer: true}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, true) }, 1200, 700)
	tt.Frame()
	tt.ClickAt(100, 300)
	tt.Frame()
	if w.drawer {
		t.Fatal("still open")
	}
	if !w.clickAt.IsZero() || w.paused {
		t.Fatal("the click reached the picture")
	}
}

// The picture takes the clicks that land on it, and what shows over it
// still takes its own: nothing in the flow may cover them.
func TestPictureTakesClicks(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	w := &Watching{app: app, index: 1, detail: &Detail{Episodes: []model.Episode{{ID: "a"}, {ID: "b"}, {ID: "c"}}}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	tt.ClickAt(200, 300)
	tt.Frame()
	if w.clickAt.IsZero() {
		t.Fatal("the picture got no click")
	}
	w.clickAt, w.halt = time.Time{}, true
	tt.Frame()
	if err := tt.Click("重播"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.halt {
		t.Fatal("the button over the picture got no click")
	}
}

// The controls of the narrowest stage still build, for the small window.
func TestStageBuildsNarrow(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	w := &Watching{app: app, index: 1, detail: &Detail{Episodes: make([]model.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, miniMinWidth, miniMinHeight)
	tt.Frame()
	tt.Frame()
}

func TestSteppedSpeed(t *testing.T) {
	for _, tt := range []struct {
		cur  float64
		d    int
		want float64
	}{{1, 1, 1.25}, {1, -1, 0.75}, {0.75, -1, 0.75}, {2, 1, 2}, {3, 1, 1}} {
		if got := steppedSpeed(tt.cur, tt.d); got != tt.want {
			t.Errorf("steppedSpeed(%v, %d) = %v, want %v", tt.cur, tt.d, got, tt.want)
		}
	}
}

// The state of the right arrow without a player: a key that went down and
// up ends the hold, and a boost asks for no seek.
func TestHoldRightEnds(t *testing.T) {
	now := time.Now()
	w := &Watching{app: &App{lib: testLibrary(t)}, rightAt: now, boost: true, boosted: true}
	w.holdRight(true, now.Add(time.Second))
	if !w.boost {
		t.Fatal("the hold ended while the key was down")
	}
	w.holdRight(false, now.Add(2*time.Second))
	if !w.rightAt.IsZero() || w.boost || w.boosted {
		t.Fatalf("still held: %+v", w)
	}
	w.holdRight(false, now.Add(3*time.Second)) // up with no press: nothing
}

// The seek bar builds and takes the pointer with no player yet.
func TestProgressWithoutPlayer(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	w := &Watching{app: app}
	tt := ui.NewTester(func(c *ui.Context) { app.progress(c, w) }, 400, 80)
	tt.Frame()
	tt.Move(100, 10)
	tt.Frame()
	tt.Frame()
}

// Narrowing never asks for less than the minimum width, so that widening
// gives back what was really taken.
func TestNarrowing(t *testing.T) {
	for in, want := range map[int]int{minWindowWidth: 0, minWindowWidth + 100: 100, 1280: panelWidth} {
		if got := narrowing(in); got != want {
			t.Errorf("narrowing(%d) = %d, want %d", in, got, want)
		}
	}
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
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t)}
	app.lib.Speed = 1
	w := &Watching{app: app, index: 1, detail: &Detail{Episodes: make([]model.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	if err := tt.Click("播放速度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.speedOpen || app.lib.Speed != 1 {
		t.Fatalf("clicked the button: open %v, speed %v", w.speedOpen, app.lib.Speed)
	}
	if a, ok := tt.AccessByRole(ui.RoleOption, "1倍"); !ok || !a.Selected {
		t.Fatalf("the current speed isn't selected: %+v", a)
	}
	if err := tt.Click("1.5倍"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.speedOpen || app.lib.Speed != 1.5 {
		t.Fatalf("chose 1.5倍: open %v, speed %v", w.speedOpen, app.lib.Speed)
	}
}
