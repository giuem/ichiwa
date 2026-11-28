package app

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// While more pages come, the grid shows whole rows only; the last page
// shows what is left, and so does a feed shorter than a row.
func TestVisibleWholeRows(t *testing.T) {
	f := &Feed{More: true}
	for i := range 13 {
		f.add([]Card{{Kind: source.CardMedia, ID: strconv.Itoa(i)}})
	}
	if got := len(Visible(f, 5)); got != 10 {
		t.Errorf("more: shows %d, want 10", got)
	}
	f.Err = "boom"
	if got := len(Visible(f, 5)); got != 13 {
		t.Errorf("failed: shows %d, want 13", got)
	}
	f.Err, f.More = "", false
	if got := len(Visible(f, 5)); got != 13 {
		t.Errorf("ended: shows %d, want 13", got)
	}
	short := &Feed{More: true, Items: make([]Card, 3)}
	if got := len(Visible(short, 5)); got != 3 {
		t.Errorf("short: shows %d, want 3", got)
	}
}

// A reset feed keeps its source, starts over, and tells loads under way
// apart from new ones.
func TestFeedReset(t *testing.T) {
	f := newFeed(&sourcetest.Source{}, func(context.Context, source.Source, string) (feedPage, error) { return feedPage{}, nil })
	f.add([]Card{{Kind: source.CardMedia, ID: "a"}})
	f.cursor, f.More, f.Loading = "c", false, true
	f.reset()
	if f.fetch == nil || !f.More || f.Loading || len(f.Items) != 0 || f.cursor != "" || f.gen != 1 {
		t.Fatalf("reset = %+v", f)
	}
	f.add([]Card{{Kind: source.CardMedia, ID: "a"}})
	if len(f.Items) != 1 {
		t.Fatal("seen survived the reset")
	}
}

// seasonsWatching is the last episode of the first of two seasons, noting
// the season it switches to.
func seasonsWatching(t *testing.T, secondOnline bool) (*Watching, *[]Season, *[]int) {
	app := &App{Lib: testLibrary(t)}
	w := &Watching{App: app, SourceID: "hongguo", Panel: true, Index: 2, Show: Card{Kind: source.CardMedia, ID: "s1"},
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

// The last episode of a season goes on to the next season's first, when
// it is out.
func TestEndedGoesToNextSeason(t *testing.T) {
	w, to, eps := seasonsWatching(t, true)
	w.onEnded()
	if len(*to) != 1 || (*to)[0].ID != "s2" || (*eps)[0] != 0 {
		t.Fatalf("switched to %v at %v", *to, *eps)
	}
	if w.Ended {
		t.Error("ended although the next season is out")
	}
	w, to, _ = seasonsWatching(t, true)
	w.Step(1)
	if len(*to) != 1 {
		t.Error("the next button didn't go to the next season")
	}
}

// A season not out yet is not switched to; the page ends and says when it
// comes out.
func TestEndedWaitsForAnnouncedSeason(t *testing.T) {
	w, to, _ := seasonsWatching(t, false)
	w.onEnded()
	if len(*to) != 0 || !w.Ended {
		t.Fatalf("switched to %v, ended %v", *to, w.Ended)
	}
}

func TestWantsDisplay(t *testing.T) {
	app := &App{}
	playing := &fakePlayer{}
	for _, c := range []struct {
		name string
		w    Watching
		want bool
	}{
		{"loading the next episode", Watching{Loading: true}, true},
		{"playing", Watching{Player: playing}, true},
		{"paused", Watching{Player: playing, Paused: true}, false},
		{"ended", Watching{Player: playing, Ended: true}, false},
		{"failed", Watching{Err: "播放失败"}, false},
		{"nothing to play", Watching{}, false},
	} {
		w := c.w
		w.App, app.Watch = app, &w
		if got := w.WantsDisplay(); got != c.want {
			t.Errorf("%s: wantsDisplay = %v, want %v", c.name, got, c.want)
		}
	}
	w := Watching{App: app, Loading: true}
	app.Watch = nil
	if w.WantsDisplay() {
		t.Error("a page left still wants the display")
	}
}

// Muting remembers the volume instead of coming back at full.
func TestMuteRestoresVolume(t *testing.T) {
	app := &App{Lib: testLibrary(t)}
	app.Lib.Volume = 0.4
	w := &Watching{App: app}
	w.ToggleMute()
	if app.Lib.Volume != 0 {
		t.Fatalf("muted at %v", app.Lib.Volume)
	}
	w.ToggleMute()
	if app.Lib.Volume != 0.4 {
		t.Fatalf("unmuted at %v", app.Lib.Volume)
	}
}

// A single click counts only after the wait; a double click goes to full
// screen and never touches the playback.
func TestPictureClicks(t *testing.T) {
	now := time.Now()
	at := func(ms int) time.Time { return now.Add(time.Duration(ms) * time.Millisecond) }
	w := &Watching{}
	if toggle, _ := w.PictureClicks(true, false, false, at(0)); toggle {
		t.Fatal("toggled before the wait")
	}
	if toggle, _ := w.PictureClicks(false, false, false, at(300)); !toggle {
		t.Fatal("the single click never counted")
	}

	w = &Watching{}
	w.PictureClicks(true, false, false, at(0))
	if toggle, full := w.PictureClicks(false, true, true, at(100)); toggle || !full {
		t.Fatalf("double click: toggle %v, full %v", toggle, full)
	}
	if toggle, _ := w.PictureClicks(true, false, false, at(150)); toggle {
		t.Fatal("the release of the double click toggled")
	}
	if toggle, _ := w.PictureClicks(false, false, false, at(900)); toggle {
		t.Fatal("a click was left waiting")
	}

	// Press and release of the second click in one frame.
	w = &Watching{}
	w.PictureClicks(true, false, false, at(0))
	if toggle, full := w.PictureClicks(true, true, false, at(100)); toggle || !full {
		t.Fatalf("one frame: toggle %v, full %v", toggle, full)
	}
	if w.skipClick {
		t.Fatal("the next click would be lost")
	}

	// Slower than the wait: the pause that happened is put back.
	w = &Watching{}
	w.PictureClicks(true, false, false, at(0))
	w.PictureClicks(false, false, false, at(250))
	if toggle, full := w.PictureClicks(false, true, true, at(450)); !toggle || !full {
		t.Fatalf("slow double click: toggle %v, full %v", toggle, full)
	}
}

// With autoplay off the page waits at the end of an episode.
func TestEndedHaltsWithoutAutoplay(t *testing.T) {
	w, to, _ := seasonsWatching(t, true)
	w.App.Lib.Autoplay = false
	w.onEnded()
	if !w.Halt || w.Ended || len(*to) != 0 {
		t.Fatalf("halt %v, ended %v, switched %v", w.Halt, w.Ended, *to)
	}
	if w.WantsDisplay() {
		t.Error("keeps the display on while waiting")
	}
}

// Stopping at the end of the episode happens once.
func TestSleepEpisodeStopsOnce(t *testing.T) {
	w, _, _ := seasonsWatching(t, true)
	w.App.SetSleep(SleepEpisode)
	w.onEnded()
	if !w.Halt || w.App.SleepMins != 0 {
		t.Fatalf("halt %v, sleep %d", w.Halt, w.App.SleepMins)
	}
}

func TestSleepTickWaitsForLoading(t *testing.T) {
	app := &App{Lib: testLibrary(t)}
	w := &Watching{App: app, Loading: true}
	app.SetSleep(15)
	if w.sleepTick(time.Now().Add(time.Hour)) || app.SleepMins != 15 {
		t.Fatal("ran out during loading")
	}
	w.Loading = false
	if w.sleepTick(time.Now()) {
		t.Fatal("ran out early")
	}
	if !w.sleepTick(time.Now().Add(time.Hour)) || app.SleepMins != 0 {
		t.Fatal("did not run out")
	}
}

func TestMiniSize(t *testing.T) {
	for _, tt := range []struct{ vw, vh, w, h int }{
		{1080, 1920, 360, 640}, {0, 0, 360, 640}, {1920, 1080, 640, 360}, {720, 2400, MiniMinWidth, 640}, {2400, 720, 640, MiniMinHeight},
	} {
		if w, h := MiniSize(tt.vw, tt.vh); w != tt.w || h != tt.h {
			t.Errorf("miniSize(%d, %d) = %d, %d", tt.vw, tt.vh, w, h)
		}
	}
}

func TestSteppedSpeed(t *testing.T) {
	for _, tt := range []struct {
		cur  float64
		d    int
		want float64
	}{{1, 1, 1.25}, {1, -1, 0.75}, {0.75, -1, 0.75}, {2, 1, 2}, {3, 1, 1}} {
		if got := SteppedSpeed(tt.cur, tt.d); got != tt.want {
			t.Errorf("steppedSpeed(%v, %d) = %v, want %v", tt.cur, tt.d, got, tt.want)
		}
	}
}

// The state of the right arrow without a player: a key that went down and
// up ends the hold, and a boost asks for no seek.
func TestHoldRightEnds(t *testing.T) {
	now := time.Now()
	w := &Watching{App: &App{Lib: testLibrary(t)}, RightAt: now, Boost: true, boosted: true}
	w.HoldRight(true, now.Add(time.Second))
	if !w.Boost {
		t.Fatal("the hold ended while the key was down")
	}
	w.HoldRight(false, now.Add(2*time.Second))
	if !w.RightAt.IsZero() || w.Boost || w.boosted {
		t.Fatalf("still held: %+v", w)
	}
	w.HoldRight(false, now.Add(3*time.Second)) // up with no press: nothing
}

// Narrowing never asks for less than the minimum width, so that widening
// gives back what was really taken.
func TestNarrowing(t *testing.T) {
	for in, want := range map[int]int{MinWindowWidth: 0, MinWindowWidth + 100: 100, 1280: PanelWidth} {
		if got := Narrowing(in); got != want {
			t.Errorf("narrowing(%d) = %d, want %d", in, got, want)
		}
	}
}

// testLibrary closes its database before the test's directory goes, which
// Windows refuses while the file is open.
func testLibrary(t *testing.T) *library.Library {
	lib := library.Load(t.TempDir())
	t.Cleanup(lib.Close)
	return lib
}

// The app never cancels its calls to the source (it drops stale results by
// generation instead), so a context.Canceled reaching it is the source
// failing: it must show an error and leave a retry, not spin or stay blank.
func TestSourceCancellationShowsError(t *testing.T) {
	done := make(chan func(), 8)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "demo", Capabilities: []source.Capability{source.CapSearch, source.CapSearchHome}},
		OnCollection: func(context.Context, source.CollectionRequest) (*source.CollectionPage, error) {
			return nil, context.Canceled
		},
		OnSearchHome: func(context.Context) (*source.CollectionPage, error) {
			return nil, context.Canceled
		},
	}
	app := &App{
		Src:    src,
		Lib:    testLibrary(t),
		post:   func(fn func()) { done <- fn },
		Search: &searchState{},
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

	app.warmed = true
	f := &app.Col("home").Results
	app.Ensure(app.Col("home"))
	drain()
	if f.Err == "" || f.Loading || !f.Loaded {
		t.Fatalf("canceled feed: err=%q loading=%v loaded=%v", f.Err, f.Loading, f.Loaded)
	}

	app.LoadHot()
	drain()
	if app.Search.HotErr == "" || app.Search.hotBusy {
		t.Fatalf("canceled hot: err=%q busy=%v", app.Search.HotErr, app.Search.hotBusy)
	}

	w := &Watching{App: app, Detail: &Media{Episodes: []source.Episode{{ID: "e1"}, {ID: "e2"}}}}
	w.failed(0, false, context.Canceled)
	if w.Err == "" || w.Skipped != 0 {
		t.Fatalf("canceled playback failed(): err=%q skipped=%d", w.Err, w.Skipped)
	}
}

// fakePlayer is a player that plays nothing.
type fakePlayer struct{ playing bool }

func (p *fakePlayer) Play()                   { p.playing = true }
func (p *fakePlayer) Pause()                  { p.playing = false }
func (p *fakePlayer) Playing() bool           { return p.playing }
func (p *fakePlayer) Ended() bool             { return false }
func (p *fakePlayer) Ready() bool             { return true }
func (p *fakePlayer) Err() error              { return nil }
func (p *fakePlayer) Duration() time.Duration { return time.Minute }
func (p *fakePlayer) Position() time.Duration { return 0 }
func (p *fakePlayer) Seek(time.Duration)      {}
func (p *fakePlayer) SetRate(float64)         {}
func (p *fakePlayer) SetVolume(float64)       {}
func (p *fakePlayer) Size() (int, int)        { return 0, 0 }
func (p *fakePlayer) Close()                  {}
