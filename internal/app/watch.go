package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"hongguo/internal/catalog"
)

// speedSteps are the speeds to choose from, in order, for the panel and
// for the keys that step them.
var speedSteps = []float64{0.75, 1, 1.25, 1.5, 2}

// boostRate is the speed while the right arrow is held, and boostDelay how
// long it takes being held rather than pressed.
const (
	boostRate  = 2
	boostDelay = 350 * time.Millisecond
)

// clickDelay is how long a click on the picture waits to see whether it is
// the first of a double click, and clickUndo how late that can turn out,
// as a slow double click goes by the system's longer interval.
const (
	clickDelay = 220 * time.Millisecond
	clickUndo  = 600 * time.Millisecond
)

// sleepEpisode is the sleep timer's choice of stopping as the episode ends.
const sleepEpisode = -1

// The smallest the small window gets, and the long side it starts at.
const (
	miniMinWidth  = 340
	miniMinHeight = 360
	miniLongSide  = 640
)

// miniFixFor is how long the small window holds its size after full screen.
const miniFixFor = 1500 * time.Millisecond

// keyLinger is how long the controls show after a key acted on the video.
const keyLinger = 1500 * time.Millisecond

// maxSkips is how many episodes in a row autoplay skips because the
// anonymous device cannot play them before it gives up and says so.
const maxSkips = 3

// retryCooldown is how long the retry button waits after the server
// limited anonymous playback: the SDK already tried fresh devices, and
// retrying at once only registers more of them.
const retryCooldown = 30 * time.Second

// episodesPerRange is how many episodes the episode grid shows at once.
const episodesPerRange = 50

// panelWidth is the width of the player page's side panel. Pinning the
// window takes it off the window.
const panelWidth = 368

// chromeLinger is how long full screen keeps the title bar and the controls
// after the pointer last moved.
const chromeLinger = 3 * time.Second

// Watching is the player page: a drama, the episode playing and the
// player playing it.
type Watching struct {
	app  *App
	show Show

	detail    *Detail
	detailErr string
	recs      []Show

	index   int
	gen     int // counts episodes started, to drop stale results
	player  *ui.VideoPlayer
	stop    chan struct{}
	loading bool
	err     string
	ended   bool
	paused  bool // the user paused
	auto    bool // the next play call comes from autoplay
	skipped int  // episodes autoplay skipped in a row
	retryAt time.Time
	awake   func() // releases the display, while it is kept on

	seek     float64
	dragging bool
	panel    bool
	unpin    bool // the panel opens again when the window is unpinned
	onTop    bool // what the window was last told
	descOpen bool
	epRange  int

	// Where the pointer was over the stage in full screen, and since when.
	pointerX, pointerY float32
	pointerAt          time.Time

	seekBar ui.Handle

	// The right arrow: when it went down, while it is, and whether holding
	// it sped the video up, which then asks for no seek as it comes up.
	rightAt        time.Time
	boost, boosted bool

	keyAt time.Time // when a key last acted on the video

	// A click on the picture waiting to count, when one last did, and
	// whether the release of a double click is still to come.
	clickAt, toggledAt time.Time
	skipClick          bool

	halt   bool // the episode ended and the page waits rather than go on
	drawer bool // the episodes are open over the picture

	side      ui.ScrollState
	dm        *danmaku // the comments of the episode playing
	dmOpen    bool     // the danmaku settings show
	speedOpen bool     // the speed picker shows
	volOpen   bool     // the volume panel shows
	told      string   // what screen readers were last told is playing

	// seasons are the drama's seasons when it has more than one, the one
	// showing among them; switchSeason opens another at an episode, -1
	// for where the user left it.
	seasons      []Season
	switchSeason func(s Season, ep int)
}

// open shows the player page for a drama, at episode ep, or where the
// user left it when ep is -1.
func (app *App) open(s Show, ep int) { app.openIn(s, ep, nil) }

// openIn opens a drama among seasons already known, which the page keeps
// rather than asks for again.
func (app *App) openIn(s Show, ep int, seasons []Season) {
	var awake func()
	if app.watch != nil {
		app.watch.close()
		// A season going on to the next keeps the display on throughout.
		awake, app.watch.awake = app.watch.awake, nil
	}
	w := &Watching{app: app, show: s, index: max(ep, 0), loading: true, panel: !app.lib.OnTop, unpin: app.lib.OnTop, awake: awake}
	w.switchSeason = func(next Season, ep int) { app.openIn(next.Show, ep, w.seasons) }
	app.watch = w
	go w.keepAwake()
	if seasonOf(seasons, s.ID) >= 0 {
		w.seasons = seasons
	} else {
		async(app, func() ([]Season, error) { return app.be.Seasons(s.ID) }, func(ss []Season, err error) {
			if app.watch != w || err != nil || seasonOf(ss, s.ID) < 0 {
				return
			}
			w.seasons = ss
			// Then what the 上新 calendar says of the seasons to come, for
			// whichever season of them shows by then.
			async(app, func() ([]Season, error) { return app.be.Announce(ss), nil }, func(ss []Season, _ error) {
				if cur := app.watch; cur != nil && len(cur.seasons) == len(ss) && seasonOf(cur.seasons, ss[0].ID) == 0 {
					cur.seasons = ss
				}
			})
		})
	}
	async(app, func() (*Detail, error) { return app.be.Series(s.ID) }, func(d *Detail, err error) {
		if app.watch != w {
			return
		}
		if err != nil {
			w.loading, w.detailErr = false, errText(err)
			return
		}
		w.detail = d
		// Keep what the grid knew when the detail lacks it.
		if d.Cover == "" {
			d.Cover = s.Cover
		}
		w.show = d.Show
		start, at := ep, 0.0
		if ep < 0 {
			start = 0
			if rec := app.lib.History[s.ID]; rec != nil {
				start, at = rec.Episode, rec.Position
				if rec.Duration > 0 && at > rec.Duration-3 { // finished: the next one
					start, at = start+1, 0
				}
			}
		}
		start = min(max(start, 0), len(d.Episodes)-1)
		w.play(start, at)
	})
	async(app, func() ([]Show, error) { return app.be.Recommend(s.ID) }, func(r []Show, err error) {
		if app.watch == w && err == nil {
			w.recs = r
		}
	})
}

func (app *App) closeWatch() {
	if app.watch != nil {
		app.miniOn = false
		app.setSleep(0)
		if app.watch.onTop { // only the player page stays on top
			app.win.SetAlwaysOnTop(false)
		}
		app.watch.close()
		app.watch = nil
		mygo.NowPlaying.Clear()
	}
}

func (w *Watching) stopPlayer() {
	w.dropDanmaku()
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
	if w.player != nil {
		w.saveProgress()
		w.player.Close()
		w.player = nil
	}
}

// close leaves the page.
func (w *Watching) close() {
	w.stopPlayer()
	w.gen++
	w.app.lib.Flush()
}

func (w *Watching) saveProgress() {
	if w.player == nil || w.detail == nil {
		return
	}
	w.app.lib.Progress(w.show, w.index, w.player.Position().Seconds(), w.player.Duration().Seconds())
}

// play starts episode i at second at.
func (w *Watching) play(i int, at float64) {
	app := w.app
	w.stopPlayer()
	w.gen++
	gen := w.gen
	auto := w.auto
	w.auto = false
	if !auto {
		w.skipped = 0
	}
	w.index, w.err, w.ended, w.paused, w.loading = i, "", false, false, true
	w.halt = false
	w.retryAt = time.Time{}
	w.boost = false
	w.epRange = i / episodesPerRange
	ep := w.detail.Episodes[i]
	if !ep.Playable {
		if auto && i+1 < len(w.detail.Episodes) {
			w.auto = true
			w.play(i+1, 0)
			return
		}
		w.loading, w.err = false, "该集暂不可播放"
		return
	}
	app.lib.Progress(w.show, i, at, 0)
	go func() {
		path, err := app.be.Episode(context.Background(), ep.ID)
		app.win.Update(func() {
			if app.watch != w || w.gen != gen {
				return
			}
			w.loading = false
			var p *ui.VideoPlayer
			if err == nil {
				p, err = video.Open(path)
			}
			if err != nil {
				w.failed(i, auto, err)
				return
			}
			w.skipped = 0
			w.player = p
			// It starts playing once the view shows it.
			p.SetVolume(app.lib.Volume).SetRate(app.lib.Speed)
			if at > 1 {
				p.Seek(seconds(at - 1))
			}
			w.stop = make(chan struct{})
			go w.watchEnd(gen, p, w.stop)
			// Control Center learns the duration once the item loaded.
			time.AfterFunc(600*time.Millisecond, func() {
				mygo.RunOnMain(func() {
					if w.gen == gen && app.watch == w {
						w.nowPlaying()
					}
				})
			})
			// The next episode downloads meanwhile, so it starts at once.
			if i+1 < len(w.detail.Episodes) && w.detail.Episodes[i+1].Playable {
				next := w.detail.Episodes[i+1].ID
				go func() {
					time.Sleep(2 * time.Second)
					_, _ = app.be.Episode(context.Background(), next)
				}()
			}
		})
	}()
}

// watchEnd notices the end of the episode. It asks the clock rather than
// wait for the view, as a window out of sight builds no frames while the
// sound plays on.
func (w *Watching) watchEnd(gen int, p *ui.VideoPlayer, stop chan struct{}) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		var failed, finished bool
		mygo.RunOnMain(func() {
			if w.gen != gen || w.app.watch != w { // the player may be closed
				return
			}
			dur := p.Duration()
			failed = p.Err() != nil
			finished = p.Ended() || dur > 0 && p.Position() >= dur-300*time.Millisecond
		})
		if !failed && !finished {
			continue
		}
		w.app.win.Update(func() {
			if w.gen != gen || w.app.watch != w {
				return
			}
			if failed {
				w.err = "播放失败"
				return
			}
			w.onEnded()
		})
		return
	}
}

// wantsDisplay tells whether the display should stay on: from the moment
// an episode is asked for until the user pauses or nothing is left to
// play, so autoplay moving on leaves no gap for an idle display to go dark.
func (w *Watching) wantsDisplay() bool {
	return w.app.watch == w && w.err == "" && !w.ended && !w.halt && !w.paused && (w.loading || w.player != nil)
}

// keepAwake holds the display on while the page wants it, until the page
// is left. AVPlayer does not do it itself, as it only prevents display
// sleep when it draws through an AVPlayerLayer, and ui.Video copies the
// frames out. It asks the clock, as a window out of sight builds no views.
func (w *Watching) keepAwake() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		left := false
		mygo.RunOnMain(func() {
			if w.sleepTick(time.Now()) {
				w.app.win.Invalidate()
			}
			switch want := w.wantsDisplay(); {
			case want && w.awake == nil:
				w.awake = mygo.Power.KeepAwake("正在播放短剧", true)
			case !want && w.awake != nil:
				w.awake()
				w.awake = nil
			}
			left = w.app.watch != w
		})
		if left {
			return
		}
		<-tick.C
	}
}

// failed shows why episode i did not play. Autoplay skips an episode the
// anonymous device cannot play, up to maxSkips in a row, as the
// openhongguo CLI does.
func (w *Watching) failed(i int, auto bool, err error) {
	app := w.app
	if auto && catalog.Unavailable(err) && i+1 < len(w.detail.Episodes) {
		w.skipped++
		if w.skipped < maxSkips {
			app.toast = fmt.Sprintf("第%d集暂时无法播放，已跳过", i+1)
			w.auto = true
			w.play(i+1, 0)
			return
		}
	}
	w.err = errText(err)
	if w.skipped >= maxSkips {
		w.err = fmt.Sprintf("连续%d集无法播放：%s", w.skipped, w.err)
	}
	if catalog.Limited(err) {
		w.retryAt = time.Now().Add(retryCooldown)
		until := w.retryAt
		go func() { // redraw the countdown each second
			for time.Now().Before(until) {
				time.Sleep(time.Second)
				app.win.Invalidate()
			}
		}()
	}
}

// retryWait is how many seconds the retry button still waits.
func (w *Watching) retryWait() int {
	if d := time.Until(w.retryAt); d > 0 {
		return int(d.Seconds()) + 1
	}
	return 0
}

func seasonOf(seasons []Season, id string) int {
	for i, s := range seasons {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// nextSeason is the season after the one showing, if there is one.
func (w *Watching) nextSeason() (Season, bool) {
	i := seasonOf(w.seasons, w.show.ID)
	if i < 0 || i+1 >= len(w.seasons) {
		return Season{}, false
	}
	return w.seasons[i+1], true
}

// lastEpisode tells whether the episode playing is the season's last.
func (w *Watching) lastEpisode() bool {
	return w.detail != nil && w.index+1 >= len(w.detail.Episodes)
}

// toNextSeason starts the next season from its first episode, if it is
// out, and tells whether it did.
func (w *Watching) toNextSeason() bool {
	next, ok := w.nextSeason()
	if !ok || !next.Online {
		return false
	}
	w.app.toast = fmt.Sprintf("接着看第%d季", next.Index)
	w.switchSeason(next, 0)
	return true
}

// hasNext tells whether something follows the episode playing.
func (w *Watching) hasNext() bool {
	if !w.lastEpisode() {
		return true
	}
	next, ok := w.nextSeason()
	return ok && next.Online
}

// sleepOptions are the sleep timer's choices, in minutes.
var sleepOptions = []struct {
	label string
	mins  int
}{{"不开启", 0}, {"播完本集", sleepEpisode}, {"15 分钟", 15}, {"30 分钟", 30}, {"60 分钟", 60}}

// setSleep sets the sleep timer to one of sleepOptions.
func (app *App) setSleep(mins int) {
	app.sleepMins, app.sleepAt = mins, time.Time{}
	if mins > 0 {
		app.sleepAt = time.Now().Add(time.Duration(mins) * time.Minute)
	}
}

// sleepTick pauses the video once the sleep timer ran out, and tells
// whether it did anything. It asks the clock, as a window out of sight
// builds no views. An episode still loading waits for its player.
func (w *Watching) sleepTick(now time.Time) bool {
	app := w.app
	if app.sleepAt.IsZero() || now.Before(app.sleepAt) || w.loading {
		return false
	}
	app.setSleep(0)
	if w.player != nil && w.player.Playing() {
		w.togglePlay()
		app.toast = "定时已到，已暂停"
	}
	return true
}

func (w *Watching) toggleAutoplay() {
	lib := w.app.lib
	lib.Autoplay = !lib.Autoplay
	lib.MarkDirty()
	w.app.toast = ternary(lib.Autoplay, "已开启自动连播", "已关闭自动连播")
}

// pictureClicks turns what the picture got this frame into what to do: a
// click waits out clickDelay, so that a double click goes to full screen
// without pausing, and one slower than that puts the pause back.
func (w *Watching) pictureClicks(clicked, double, pressed bool, now time.Time) (toggle, full bool) {
	if double {
		full, w.skipClick = true, true
		if w.clickAt.IsZero() && !w.toggledAt.IsZero() && now.Sub(w.toggledAt) < clickUndo {
			toggle = true
		}
		w.clickAt, w.toggledAt = time.Time{}, time.Time{}
	}
	switch {
	case clicked && w.skipClick:
		w.skipClick = false
	case clicked:
		w.clickAt = now
	case !pressed:
		w.skipClick = false // the second press let go elsewhere
	}
	if !w.clickAt.IsZero() && now.Sub(w.clickAt) >= clickDelay {
		w.clickAt, w.toggledAt, toggle = time.Time{}, now, true
	}
	return toggle, full
}

func (w *Watching) onEnded() {
	if w.player != nil {
		w.saveProgress()
	}
	app := w.app
	once := app.sleepMins == sleepEpisode
	if once {
		app.setSleep(0)
	}
	if w.hasNext() && (once || !app.lib.Autoplay) {
		w.halt = true
		if p := w.player; p != nil {
			p.Pause()
			w.syncNowPlaying()
		}
		return
	}
	if w.index+1 < len(w.detail.Episodes) {
		w.auto = true
		w.play(w.index+1, 0)
		return
	}
	if w.toNextSeason() {
		return
	}
	w.ended = true
}

func (w *Watching) togglePlay() {
	p := w.player
	if p == nil {
		return
	}
	if w.ended || w.halt { // the watch on its end is over: start it anew
		w.play(w.index, 0)
		return
	}
	if p.Playing() {
		p.Pause()
		w.paused = true
		w.saveProgress()
	} else {
		p.Play()
		w.paused = false
	}
	w.syncNowPlaying()
}

// toggleOnTop pins the window above others, or unpins it. A pinned window
// is out of the way as far as it can be, so the panel folds, and opens
// again on unpinning unless the user touched it since.
func (w *Watching) toggleOnTop() {
	lib := w.app.lib
	lib.OnTop = !lib.OnTop
	lib.MarkDirty()
	switch {
	case lib.OnTop && w.panel:
		w.panel, w.unpin = false, true
	case !lib.OnTop && w.unpin:
		w.panel, w.unpin = true, false
	}
	w.app.toast = ternary(lib.OnTop, "窗口已置顶", "已取消置顶")
}

// syncOnTop tells the window to stay on top or not: a full screen window
// has nothing to stay above.
func (w *Watching) syncOnTop(full bool) {
	if want := w.app.lib.OnTop && !full; want != w.onTop {
		w.onTop = want
		w.app.win.SetAlwaysOnTop(want)
	}
}

// syncNarrow takes the width of a panel folded by pinning off the window,
// as far as the minimum size lets it, and gives that back when the panel
// opens, the window unpins or the page closes. It waits out full screen,
// which keeps its own size, and leaves a maximized window alone.
func (app *App) syncNarrow() {
	if app.miniOn || app.mini { // that one has the size until it ends
		return
	}
	w := app.watch
	want := w != nil && app.lib.OnTop && w.unpin
	if want == app.narrowed || app.win.IsFullScreen() || want && app.win.IsMaximized() {
		return
	}
	app.narrowed = want
	// After this build: resizing makes a frame of its own.
	app.win.Update(func() {
		width, height := app.win.Size()
		if want {
			app.narrowedBy = narrowing(width)
			app.win.SetSize(width-app.narrowedBy, height)
		} else {
			app.win.SetSize(width+app.narrowedBy, height)
		}
	})
}

// widen gives a narrowed window its width back at once, as the app quits,
// so that the size saved for the next launch is the usual one.
func (app *App) widen() {
	if app.mini && !app.win.IsFullScreen() {
		app.mini = false
		app.win.SetSize(app.miniBackW, app.miniBackH)
		app.win.SetMinimumSize(minWindowWidth, minWindowHeight)
	}
	if !app.narrowed || app.win.IsFullScreen() {
		return
	}
	app.narrowed = false
	width, height := app.win.Size()
	app.win.SetSize(width+app.narrowedBy, height)
}

// toggleMini asks for the small window that fits the video, or for the
// window it was.
func (w *Watching) toggleMini() {
	app := w.app
	if app.win.IsFullScreen() {
		return
	}
	app.miniOn = !app.miniOn
	app.toast = ternary(app.miniOn, "已进入小窗", "已退出小窗")
}

// syncMini resizes the window for the small window, or back from it. Like
// syncNarrow, it waits out full screen.
func (app *App) syncMini() {
	want := app.watch != nil && app.miniOn
	if app.win.IsFullScreen() {
		app.miniFull = app.mini
		return
	}
	// Leaving full screen does not bring a window that small back, and its
	// animation undoes a size set while it runs, so keep at it until it ended.
	if app.miniFull {
		app.miniFull = false
		app.miniFixUntil = time.Now().Add(miniFixFor)
		for _, d := range []time.Duration{miniFixFor / 2, miniFixFor} { // a paused video draws no frames
			time.AfterFunc(d, app.win.Invalidate)
		}
	}
	if want && app.mini && time.Now().Before(app.miniFixUntil) {
		if w, h := app.win.Size(); w != app.miniW || h != app.miniH {
			app.win.Update(func() { app.win.SetSize(app.miniW, app.miniH) })
		}
		return
	}
	if want == app.mini {
		return
	}
	app.mini = want
	vw, vh := 0, 0
	if want && app.watch.player != nil && app.watch.player.Ready() {
		vw, vh = app.watch.player.Size()
	}
	// After this build: resizing makes a frame of its own.
	app.win.Update(func() {
		if want {
			app.miniBackW, app.miniBackH = app.win.Size()
			app.win.SetMinimumSize(miniMinWidth, miniMinHeight)
			app.miniW, app.miniH = miniSize(vw, vh)
			app.win.SetSize(app.miniW, app.miniH)
		} else {
			app.win.SetSize(app.miniBackW, app.miniBackH)
			app.win.SetMinimumSize(minWindowWidth, minWindowHeight)
		}
	})
}

// miniSize is the small window for a video of this size, portrait when
// the size is not known yet.
func miniSize(vw, vh int) (int, int) {
	if vw <= 0 || vh <= 0 {
		vw, vh = 9, 16
	}
	if vh >= vw {
		return max(miniLongSide*vw/vh, miniMinWidth), miniLongSide
	}
	return miniLongSide, max(miniLongSide*vh/vw, miniMinHeight)
}

// drawerFits tells the episodes open over the picture here: full screen
// and a window with no room for the side panel have nowhere else for them.
func drawerFits(width float32, full bool) bool { return full || !panelFits(width) }

// toggleEpisodes opens or closes the episodes, wherever they go here.
func (w *Watching) toggleEpisodes(width float32, full bool) {
	if drawerFits(width, full) {
		w.drawer = !w.drawer
	} else {
		w.panel, w.unpin = !w.panel, false
	}
}

// narrowing is how much of the panel's width a window this wide can give up.
func narrowing(width int) int {
	return max(min(panelWidth, width-minWindowWidth), 0)
}

// panelFits tells a window this wide has room for the side panel beside
// the controls of the video.
func panelFits(width float32) bool { return width >= minWindowWidth+panelWidth }

func (w *Watching) step(d int) {
	if w.detail == nil {
		return
	}
	i := w.index + d
	if i >= 0 && i < len(w.detail.Episodes) {
		w.play(i, 0)
	} else if d > 0 {
		w.toNextSeason()
	}
}

func (w *Watching) seekBy(d float64) {
	if p := w.player; p != nil {
		p.Seek(min(max(p.Position()+seconds(d), 0), p.Duration()))
		w.keyAt = time.Now()
		w.syncNowPlaying()
	}
}

// holdRight follows the right arrow as it is held: a press seeks as it
// comes up, a hold plays faster until it does.
func (w *Watching) holdRight(down bool, now time.Time) {
	switch {
	case w.rightAt.IsZero():
	case !down:
		if w.boost && w.player != nil {
			w.player.SetRate(w.app.lib.Speed)
		}
		if !w.boosted {
			w.seekBy(5)
		}
		w.rightAt, w.boost, w.boosted = time.Time{}, false, false
	case !w.boost && now.Sub(w.rightAt) >= boostDelay && w.player != nil && w.player.Playing():
		w.player.SetRate(boostRate)
		w.boost, w.boosted = true, true
	}
}

// resumeAt is where a retry of the episode picks up.
func (w *Watching) resumeAt() float64 {
	if w.player != nil {
		return w.player.Position().Seconds()
	}
	if rec := w.app.lib.History[w.show.ID]; rec != nil && rec.Episode == w.index {
		return rec.Position
	}
	return 0
}

func (w *Watching) setVolume(v float64) {
	lib := w.app.lib
	lib.Volume = min(max(v, 0), 1)
	lib.MarkDirty()
	if p := w.player; p != nil {
		p.SetVolume(lib.Volume)
	}
	w.keyAt = time.Now()
}

// toggleMute silences the video, or brings back the volume it had.
func (w *Watching) toggleMute() {
	app := w.app
	if app.lib.Volume > 0 {
		app.unmute = app.lib.Volume
		w.setVolume(0)
		app.toast = "已静音"
		return
	}
	w.setVolume(ternary(app.unmute > 0, app.unmute, 1.0))
	app.toast = volumeText(app.lib.Volume)
}

func (w *Watching) nudgeVolume(d float64) {
	w.setVolume(w.app.lib.Volume + d)
	w.app.toast = volumeText(w.app.lib.Volume)
}

func volumeText(v float64) string { return fmt.Sprintf("音量 %d%%", int(v*100+0.5)) }

func (w *Watching) setSpeed(next float64) {
	app := w.app
	app.lib.Speed = next
	app.lib.MarkDirty()
	if p := w.player; p != nil && !w.boost {
		p.SetRate(next)
	}
	w.keyAt = time.Now()
	app.toast = fmt.Sprintf("播放速度 %gx", next)
}

// stepSpeed moves the speed d steps along speedSteps, stopping at its ends.
func (w *Watching) stepSpeed(d int) { w.setSpeed(steppedSpeed(w.app.lib.Speed, d)) }

func steppedSpeed(cur float64, d int) float64 {
	for i, s := range speedSteps {
		if s == cur {
			return speedSteps[min(max(i+d, 0), len(speedSteps)-1)]
		}
	}
	return 1
}

// seconds is s seconds as a Duration.
func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func fmtTime(s float64) string {
	if s < 0 {
		s = 0
	}
	n := int(s + 0.5)
	if n >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", n/3600, n/60%60, n%60)
	}
	return fmt.Sprintf("%02d:%02d", n/60, n%60)
}

func ago(r *Record) string {
	d := time.Since(r.Updated)
	switch {
	case d < time.Minute:
		return "刚刚看过"
	case d < time.Hour:
		return fmt.Sprintf("%d分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d天前", int(d.Hours()/24))
	}
	return r.Updated.Format("2006-01-02")
}

func (app *App) watchView(c *ui.Context) {
	w := app.watch
	// A click leaves the focus alone, or the button clicked would take Space
	// and the sliders the arrows from the shortcuts below. Tab still reaches them.
	c.Root().Background(app.pal.bg).KeepFocus()
	full := app.win.IsFullScreen()
	w.syncOnTop(full)

	// Keyboard.
	c.OnShortcut(0, ui.KeySpace, w.togglePlay)
	c.OnShortcut(0, ui.KeyLeft, func() { w.seekBy(-5) })
	c.OnShortcut(0, ui.KeyRight, func() {
		if w.rightAt.IsZero() {
			w.rightAt = time.Now()
		}
	})
	w.holdRight(c.KeyDown(ui.KeyRight), time.Now())
	if !w.rightAt.IsZero() && !w.boost {
		c.After(50 * time.Millisecond) // to notice the hold
	}
	c.OnShortcut(0, ui.KeyUp, func() { w.step(-1) })
	c.OnShortcut(0, ui.KeyDown, func() { w.step(1) })
	c.OnShortcut(0, ui.KeyF, app.win.ToggleFullScreen)
	width, _ := c.Size()
	if !drawerFits(width, full) || w.detail == nil {
		w.drawer = false
	}
	c.OnShortcut(0, ui.KeyE, func() { w.toggleEpisodes(width, full) })
	c.OnShortcut(0, ui.KeyA, w.toggleAutoplay)
	c.OnShortcut(0, ui.KeyD, w.toggleDanmaku)
	c.OnShortcut(0, ui.KeyV, w.toggleMini)
	c.OnShortcut(0, ui.KeyM, w.toggleMute)
	c.OnShortcut(0, ui.KeyMinus, func() { w.nudgeVolume(-0.1) })
	c.OnShortcut(0, ui.KeyEqual, func() { w.nudgeVolume(0.1) })
	c.OnShortcut(0, ui.KeyBracketLeft, func() { w.stepSpeed(-1) })
	c.OnShortcut(0, ui.KeyBracketRight, func() { w.stepSpeed(1) })
	c.OnShortcut(0, ui.KeyT, func() {
		if !app.win.IsFullScreen() {
			w.toggleOnTop()
		}
	})
	c.OnShortcut(0, ui.KeyEscape, func() {
		if w.drawer {
			w.drawer = false
			return
		}
		if app.win.IsFullScreen() {
			app.win.SetFullScreen(false)
			return
		}
		app.closeWatch()
	})

	if news := w.news(); news != "" && news != w.told {
		w.told = news
		c.Announce(news)
	}

	playing := w.player != nil && w.player.Playing()
	if w.player != nil {
		if playing {
			c.After(250 * time.Millisecond) // the clock and the progress bar
		}
		if !w.dragging {
			w.saveProgress()
		}
	}

	w.syncDanmaku()

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		app.stage(c, w, playing, full)
		if w.panel && !full && app.panelRoom(c) {
			app.sidePanel(c, w)
		}
	})
}

// news is what screen readers are told as it changes: the episode that
// started, and the end. Failures are read as the alerts they show as.
func (w *Watching) news() string {
	switch {
	case w.detail == nil || w.loading || w.err != "":
		return ""
	case w.ended:
		return w.show.Title + " 已看完"
	case w.halt:
		return fmt.Sprintf("第%d集播完", w.index+1)
	}
	return fmt.Sprintf("正在播放 %s 第%d集", w.show.Title, w.index+1)
}

// playPause draws the icon of a button that plays and pauses: the icon
// going shrinks and fades as the one coming grows in, with a little spring.
func playPause(c *ui.Context, b ui.Element, playing bool, size float32) {
	v := b.AnimateTo("playing", boolf(playing), ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.3}})
	ui.Box(c).Size(size, size).Children(func() {
		for _, icon := range []struct {
			name  string
			shown float32
		}{{"play.fill", 1 - v}, {"pause.fill", v}} {
			if icon.shown <= 0.01 {
				continue
			}
			s := 0.4 + 0.6*icon.shown
			sfIcon(c, icon.name).Size(size).Color(white).Absolute().Top(0).Left(0).Scale(s, s).Opacity(min(icon.shown, 1))
		}
	})
}

// panelRoom tells the window has room for the side panel now.
func (app *App) panelRoom(c *ui.Context) bool {
	width, _ := c.Size()
	return panelFits(width)
}

// pointerStill is how long the pointer has rested on the stage, which only
// counts while the video plays and the user is not seeking. The frames that
// let the controls go are those playing asks for anyway.
func (w *Watching) pointerStill(stage ui.Element, playing bool) time.Duration {
	x, y, _ := stage.PointerPosition()
	now := time.Now()
	if x != w.pointerX || y != w.pointerY || !playing || w.dragging {
		w.pointerX, w.pointerY, w.pointerAt = x, y, now
	}
	return now.Sub(w.pointerAt)
}

// chromeShown tells whether the title bar and the controls show. A paused
// video keeps them. Otherwise the pointer brings them by moving, over the
// video in a window; in full screen it is always over the video.
func chromeShown(playing, hover, full bool, still time.Duration) bool {
	switch {
	case !playing:
		return true
	case full:
		return still < chromeLinger
	}
	return hover && still < chromeLinger
}

// stage is the video with its title bar and controls.
func (app *App) stage(c *ui.Context, w *Watching, playing, full bool) {
	bar := c.TitleBar()
	stage := ui.Column(c).Grow(1).Background(black)
	hover := stage.Hovered()
	still := w.pointerStill(stage, playing)
	// The keyboard's focus on a control keeps the controls in view.
	shown := chromeShown(playing, hover, full, still) || time.Since(w.keyAt) < keyLinger || w.drawer || w.dmOpen || w.speedOpen || w.volOpen || stage.FocusWithin()
	if !shown {
		stage.Cursor(ui.CursorNone)
	}
	show := stage.AnimateTo("chrome", boolf(shown), ui.Smooth)
	stage.Children(func() {
		// The picture, under everything.
		// Under the title bar and the controls, which are in the flow: absolute
		// elements otherwise paint over those and take their clicks.
		video := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().ZIndex(-1)
		video.Label("画面").ContextMenu(app.playerMenu(w, full))
		toggle, toFull := w.pictureClicks(video.Clicked(), video.DoubleClicked(), video.Pressed(), time.Now())
		if toggle {
			w.togglePlay()
		}
		if toFull {
			app.win.ToggleFullScreen()
		}
		if !w.clickAt.IsZero() {
			c.After(clickDelay)
		}
		video.Children(func() {
			// Built before its first picture too: only then does it start.
			if w.player != nil {
				ui.Video(c, w.player).Fill().Fit(ui.Contain)
			}
			app.danmakuLayer(c, w)
			if w.player == nil || !w.player.Ready() {
				if bmp := app.posters.Get(w.show.Cover); bmp != nil {
					ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Opacity(0.3).Blur(40)
				}
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(14).PassThrough().Children(func() {
				app.stageMessage(c, w)
			})
			if w.boost {
				ui.Box(c).Absolute().Top(64).Left(0).Right(0).Center().PassThrough().Children(func() {
					ui.Row(c).Gap(5).Padding(6, 12).Radius(999).Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
						sfIcon(c, "forward.fill").Size(12).Color(white)
						ui.Text(c, fmt.Sprintf("%gx 快进中", float64(boostRate))).FontSize(12.5).FontWeight(600).TextColor(white)
					})
				})
			}
		})

		// Title bar.
		ui.Row(c).Padding(max(bar.Height, 28)/2-2, bar.Right+14, 34, bar.Left+14).Gap(8).DragWindow().
			Gradient(ui.RGBA(0, 0, 0, 0.7), ui.Transparent, 180).Opacity(max(show, boolf(!playing))).
			Children(func() {
				if symbolButton(c, "chevron.left", 15, white, "返回 (Esc)").KeyShortcuts(0, ui.KeyEscape).Clicked() {
					app.closeWatch()
				}
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, w.show.Title).FontSize(15).Bold().TextColor(white).SingleLine().Role(ui.RoleHeading).Level(1)
					if w.detail != nil {
						ui.Text(c, fmt.Sprintf("第%d集 · 共%d集", w.index+1, len(w.detail.Episodes))).FontSize(11.5).TextColor(white.Alpha(0.65))
					}
				})
				if !full && app.panelRoom(c) && symbolButton(c, "sidebar.right", 15, white, "选集面板").Expanded(w.panel).Clicked() {
					w.panel, w.unpin = !w.panel, false
				}
			})

		// Painted above the picture, which is under the flow: it must let the
		// pointer through or the picture gets no clicks.
		ui.Spacer(c).PassThrough()

		// Controls.
		// What does not fit a narrow stage goes first, for keys and the menu.
		width, _ := c.Size()
		stageWidth := width
		if w.panel && !full && app.panelRoom(c) {
			stageWidth -= panelWidth
		}
		tiny := stageWidth < 460
		ui.Column(c).Padding(40, 18, 12).Gap(4).Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.78), 180).Opacity(show).Children(func() {
			app.progress(c, w)
			ui.Row(c).Gap(2).Children(func() {
				hasPrev := w.detail != nil && w.index > 0
				next, hasNextSeason := w.nextSeason()
				hasNextSeason = hasNextSeason && next.Online && w.lastEpisode()
				hasNext := w.detail != nil && w.index+1 < len(w.detail.Episodes) || hasNextSeason
				if !tiny && symbolButton(c, "backward.end.fill", 15, white, "上一集 (↑)").KeyShortcuts(0, ui.KeyUp).Disabled(!hasPrev).Clicked() {
					w.step(-1)
				}
				pb := roundButton(c, 22, white, ternary(playing, "暂停 (空格)", "播放 (空格)")).KeyShortcuts(0, ui.KeySpace)
				pb.Children(func() { playPause(c, pb, playing, 22) })
				if pb.Clicked() {
					w.togglePlay()
				}
				if !tiny && symbolButton(c, "forward.end.fill", 15, white, ternary(hasNextSeason, "下一季 (↓)", "下一集 (↓)")).KeyShortcuts(0, ui.KeyDown).Disabled(!hasNext).Clicked() {
					w.step(1)
				}
				if pl := w.player; pl != nil {
					ui.Row(c).Gap(4).Margin(0, 0, 0, 8).Children(func() {
						ui.Text(c, fmtTime(w.seek)).FontSize(12.5).FontWeight(500).TextColor(white).NoWrap()
						ui.Text(c, "/ "+fmtTime(pl.Duration().Seconds())).FontSize(12.5).TextColor(white.Alpha(0.55)).NoWrap()
					})
				}
				ui.Spacer(c)
				app.speedButton(c, w)
				if !tiny {
					app.volume(c, w)
				} else {
					w.volOpen = false
				}
				if symbolButton(c, "text.bubble.fill", 14, ternary(app.lib.Danmaku, app.pal.accent, white), "弹幕 (D)").KeyShortcuts(0, ui.KeyD).Role(ui.RoleToggleButton).Checked(app.lib.Danmaku).Clicked() {
					w.toggleDanmaku()
				}
				if app.lib.Danmaku && !tiny {
					sb := symbolButton(c.Key("dm-settings"), "slider.horizontal.3", 14, ternary(w.dmOpen, app.pal.accent, white), "弹幕设置").Expanded(w.dmOpen)
					if sb.Clicked() {
						w.dmOpen = !w.dmOpen
					}
					app.danmakuSettings(c, w, sb)
				} else {
					w.dmOpen = false
				}
				if drawerFits(width, full) && w.detail != nil && symbolButton(c, "list.number", 14, ternary(w.drawer, app.pal.accent, white), "选集 (E)").KeyShortcuts(0, ui.KeyE).Clicked() {
					w.toggleEpisodes(width, full)
				}
				symbolButton(c.Key("more"), "ellipsis", 14, white, "更多").Menu(app.playerMenu(w, full))
				fs := ternary(full, "arrow.down.right.and.arrow.up.left", "arrow.up.left.and.arrow.down.right")
				if !full && !tiny && symbolButton(c, "pin.fill", 14, ternary(app.lib.OnTop, app.pal.accent, white), "窗口置顶 (T)").KeyShortcuts(0, ui.KeyT).Role(ui.RoleToggleButton).Checked(app.lib.OnTop).Clicked() {
					w.toggleOnTop()
				}
				if symbolButton(c, fs, 14, white, ternary(full, "退出全屏 (F)", "全屏 (F)")).KeyShortcuts(0, ui.KeyF).Clicked() {
					app.win.ToggleFullScreen()
				}
			})
		})

		if w.drawer {
			app.drawer(c, w, width)
		}
	})
}

// drawer shows the episodes over the picture. A click beside it closes it,
// and does not reach the picture.
func (app *App) drawer(c *ui.Context, w *Watching, width float32) {
	p := app.pal
	if ui.Box(c.Key("drawer-scrim")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Label("关闭选集").Clicked() {
		w.drawer = false
	}
	ui.Column(c.Key("drawer")).Absolute().Top(0).Right(0).Bottom(0).Width(min(panelWidth, width-24)).
		Background(p.bg).BorderWidth(0, 0, 0, 1).BorderColor(p.hairline).Children(func() {
		ui.Scroll(c.Key("drawer-list")).Grow(1).Padding(max(c.TitleBar().Height, 28)+12, 20, 28).Gap(24).Children(func() {
			app.seasonRow(c, w)
			app.episodes(c, w)
		})
	})
}

// playerMenu fills the menu of the controls and of the picture.
func (app *App) playerMenu(w *Watching, full bool) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("自动连播").Checked(app.lib.Autoplay).Trailing("A").Chosen() {
			w.toggleAutoplay()
		}
		if m.Item("弹幕").Checked(app.lib.Danmaku).Trailing("D").Chosen() {
			w.toggleDanmaku()
		}
		sleep := m.Submenu("定时停止", func(m *ui.Menu) {
			for _, o := range sleepOptions {
				if m.Item(o.label).Radio(app.sleepMins == o.mins).Chosen() {
					app.setSleep(o.mins)
					app.toast = ternary(o.mins == 0, "已关闭定时停止", "定时停止："+o.label)
				}
			}
		})
		if app.sleepMins == sleepEpisode {
			sleep.Trailing("本集")
		} else if !app.sleepAt.IsZero() {
			sleep.Trailing(fmt.Sprintf("%d 分钟", int(time.Until(app.sleepAt).Minutes())+1))
		}
		if full {
			return
		}
		m.Separator()
		if m.Item("小窗模式").Checked(app.miniOn).Trailing("V").Chosen() {
			w.toggleMini()
		}
		if m.Item("窗口置顶").Checked(app.lib.OnTop).Trailing("T").Chosen() {
			w.toggleOnTop()
		}
	}
}

// stageMessage is what shows over the picture: loading, errors, the end.
func (app *App) stageMessage(c *ui.Context, w *Watching) {
	dim := white.Alpha(0.7)
	switch {
	case w.detailErr != "":
		sfIcon(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.detailErr).FontSize(15).TextColor(white).Role(ui.RoleAlert)
		app.secondary(c, nil, "返回").OnClick(app.closeWatch)
	case w.err != "":
		sfIcon(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.err).FontSize(15).TextColor(white).Role(ui.RoleAlert)
		ui.Row(c).Gap(10).Margin(6, 0, 0).Children(func() {
			label := "重试"
			wait := w.retryWait()
			if wait > 0 {
				label = fmt.Sprintf("重试 (%ds)", wait)
			}
			app.secondary(c, iconRefresh, label).Disabled(wait > 0).OnClick(func() {
				if w.retryWait() == 0 {
					w.play(w.index, w.resumeAt())
				}
			})
			if w.detail != nil && w.index+1 < len(w.detail.Episodes) {
				app.primary(c, nil, "下一集").OnClick(func() { w.step(1) })
			} else if next, ok := w.nextSeason(); ok && next.Online {
				app.primary(c, nil, "下一季").OnClick(func() { w.step(1) })
			}
		})
	case w.loading:
		ui.Spinner(c).Size(30, 30)
		label := "正在加载…"
		if w.detail != nil {
			label = fmt.Sprintf("正在加载第%d集…", w.index+1)
		}
		ui.Text(c, label).FontSize(13).TextColor(dim)
	case w.halt:
		ui.Text(c, fmt.Sprintf("第%d集播完", w.index+1)).FontSize(24).Bold().TextColor(white)
		ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
			app.secondary(c, iconRefresh, "重播").OnClick(func() { w.play(w.index, 0) })
			app.primary(c, nil, ternary(w.lastEpisode(), "下一季", "下一集")).OnClick(func() { w.step(1) })
		})
	case w.ended:
		next, more := w.nextSeason()
		if !more {
			ui.Text(c, "全剧终").FontSize(24).Bold().TextColor(white)
			ui.Text(c, "已看完全部剧集").FontSize(13).TextColor(dim)
			app.primary(c, iconRefresh, "从头再看").Margin(8, 0, 0).OnClick(func() { w.play(0, 0) })
			return
		}
		cur := w.seasons[seasonOf(w.seasons, w.show.ID)]
		ui.Text(c, fmt.Sprintf("第%d季完", cur.Index)).FontSize(24).Bold().TextColor(white)
		ui.Text(c, seasonLine(next)).FontSize(13).TextColor(dim)
		ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
			app.secondary(c, iconRefresh, "从头再看").OnClick(func() { w.play(0, 0) })
			switch {
			case next.Online:
				app.primary(c, nil, fmt.Sprintf("看第%d季", next.Index)).OnClick(func() { w.switchSeason(next, -1) })
			case app.lib.Following(next.ID):
				app.secondary(c, nil, fmt.Sprintf("已追第%d季", next.Index)).OnClick(func() { app.toggleFollow(next.Show) })
			default:
				app.primary(c, iconHeart, fmt.Sprintf("追第%d季", next.Index)).OnClick(func() { app.toggleFollow(next.Show) })
			}
		})
	case w.paused && w.player != nil:
		ui.Box(c).Size(76, 76).Radius(999).Background(ui.RGBA(0, 0, 0, 0.45)).Center().
			Border(1, white.Alpha(0.15)).Children(func() {
			sfIcon(c, "play.fill").Size(30).Color(white).Margin(0, 0, 0, 4)
		})
	}
}

func (app *App) speedButton(c *ui.Context, w *Watching) {
	speed := app.lib.Speed
	b := ui.ButtonBase(c.Key("speed")).Height(30).MinWidth(30).PaddingX(8).Radius(999).Center().Label("播放速度").
		Value(fmt.Sprintf("%g倍", speed)).Expanded(w.speedOpen)
	tooltip(c, b, "播放速度 ([ / ])")
	pressable(b, ui.Transparent, white.Alpha(0.14), 0.92)
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%gx", speed)).FontSize(12.5).Bold().TextColor(ternary(w.speedOpen, app.pal.accent, white))
	})
	if b.Clicked() {
		w.speedOpen = !w.speedOpen
	}
	app.speedPicker(c, w, b)
}

// speedPicker is the list of speeds that opens from anchor, the fastest on
// top; choosing one closes it.
func (app *App) speedPicker(c *ui.Context, w *Watching, anchor ui.Element) {
	ui.PopoverBase(c, anchor, &w.speedOpen, func(panel ui.Element) {
		popPanel(panel, anchor).Width(96).Padding(6).Gap(2).Radius(12).Role(ui.RoleListBox).Label("播放速度")
		panel.Children(func() {
			for i := len(speedSteps) - 1; i >= 0; i-- {
				s := speedSteps[i]
				on := s == app.lib.Speed
				o := ui.ButtonBase(c.Key(i)).Height(30).Radius(8).Center().Role(ui.RoleOption).
					Label(fmt.Sprintf("%g倍", s)).Checked(on)
				pressable(o, ternary(on, white.Alpha(0.1), ui.Transparent), white.Alpha(0.14), 0.96)
				o.Children(func() {
					ui.Text(c, fmt.Sprintf("%gx", s)).FontSize(13).FontWeight(ternary(on, 700, 500)).
						TextColor(ternary(on, app.pal.accent, white))
				})
				if o.Clicked() {
					w.setSpeed(s)
					w.speedOpen = false
				}
			}
		})
	})
}

// volume is the speaker button, which opens a panel of the volume: how
// loud it is, a slider and a button to mute.
func (app *App) volume(c *ui.Context, w *Watching) {
	vol := app.lib.Volume
	b := symbolButton(c.Key("volume"), volumeSymbol(vol), 14, ternary(w.volOpen, app.pal.accent, white), "音量 (M 静音)").
		Value(fmt.Sprintf("%d%%", int(vol*100+0.5))).Expanded(w.volOpen)
	if b.Clicked() {
		w.volOpen = !w.volOpen
	}
	ui.PopoverBase(c, b, &w.volOpen, func(panel ui.Element) {
		popPanel(panel, b).Width(52).Padding(12, 0, 8).Gap(8).Radius(12).AlignItems(ui.Center).Label("音量")
		panel.Children(func() {
			ui.Text(c, fmt.Sprintf("%d", int(app.lib.Volume*100+0.5))).FontSize(12.5).FontWeight(600).TextColor(white)
			s := ui.SliderBase(c.Key("slider"), &app.lib.Volume, 0, 1).Vertical().Width(24).Height(110).PaddingY(7).
				Label("音量").Value(fmt.Sprintf("%d%%", int(app.lib.Volume*100+0.5)))
			if s.Changed() {
				app.lib.MarkDirty()
				if p := w.player; p != nil {
					p.SetVolume(app.lib.Volume)
				}
			}
			hot := s.AnimateTo("hot", boolf(s.Hovered() || s.Dragging()), ui.Smooth)
			at := float32(app.lib.Volume)
			s.Draw(func(pt *ui.Painter, r ui.Rect) {
				const knob = 14
				x := r.X + r.W/2
				y0, h := r.Y+knob/2, r.H-knob
				pt.Fill(ui.Rect{X: x - 2, Y: r.Y, W: 4, H: r.H}, white.Alpha(0.18), 2)
				fill := knob/2 + h*at
				pt.Fill(ui.Rect{X: x - 2, Y: r.Y + r.H - fill, W: 4, H: fill}, white.Alpha(0.85), 2)
				k := knob + 2*hot
				ky := y0 + h*(1-at)
				pt.Fill(ui.Rect{X: x - k/2, Y: ky - k/2, W: k, H: k}, white, k/2)
			})
			if symbolButton(c.Key("mute"), volumeSymbol(app.lib.Volume), 13, white.Alpha(0.85), ternary(app.lib.Volume > 0, "静音", "取消静音")).Clicked() {
				w.toggleMute()
			}
		})
	})
}

// volumeSymbol is the speaker that shows how loud vol is.
func volumeSymbol(vol float64) string {
	switch {
	case vol == 0:
		return "speaker.slash.fill"
	case vol < 0.5:
		return "speaker.wave.1.fill"
	}
	return "speaker.wave.2.fill"
}

// popPanel gives a panel of the player's controls their look and centers
// it a little above anchor, the button that opens it.
func popPanel(panel, anchor ui.Element) ui.Element {
	return panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 8, 0).
		Background(ui.RGBA(30, 30, 32, 0.97)).Border(1, white.Alpha(0.08)).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.4))
}

// spokenTime is a time as screen readers should say it.
func spokenTime(s float64) string {
	n := int(max(s, 0) + 0.5)
	if n >= 3600 {
		return fmt.Sprintf("%d小时%d分%d秒", n/3600, n/60%60, n%60)
	}
	return fmt.Sprintf("%d分%d秒", n/60, n%60)
}

// progress is the seek bar.
func (app *App) progress(c *ui.Context, w *Watching) {
	pl := w.player
	dur := 1.0
	if pl != nil {
		dur = max(pl.Duration().Seconds(), 1)
		if !w.dragging {
			w.seek = pl.Position().Seconds()
		}
	} else {
		w.seek = 0
	}
	box := ui.Box(c)
	var s ui.Element
	box.Children(func() {
		s = ui.SliderBase(c.Key("seek"), &w.seek, 0, dur).Height(20).Label("进度").Disabled(pl == nil).Bind(&w.seekBar).
			Value(spokenTime(w.seek) + "，共" + spokenTime(dur))
		// The time under the pointer, over the bar.
		x, _, over := s.PointerPosition()
		if width := w.seekBar.Bounds(c).W; pl != nil && width > 0 && (over || s.Dragging()) {
			const tip = 56
			x = min(max(x, 0), width)
			ui.Box(c).Width(tip).Padding(3, 0).Radius(5).Center().Background(ui.RGBA(0, 0, 0, 0.72)).
				Absolute().Bottom(24).Left(min(max(x-tip/2, 0), width-tip)).PassThrough().Children(func() {
				ui.Text(c, fmtTime(float64(x/width)*dur)).FontSize(11.5).FontWeight(600).TextColor(white).NoWrap()
			})
		}
	})
	if s.Changed() && pl != nil {
		pl.Seek(seconds(w.seek))
	}
	if w.dragging && !s.Dragging() {
		w.syncNowPlaying()
	}
	w.dragging = s.Dragging()
	hot := s.AnimateTo("hot", boolf(s.Hovered() || w.dragging), ui.Smooth)
	at := float32(w.seek / dur)
	accent := app.pal.accent
	s.Draw(func(pt *ui.Painter, r ui.Rect) {
		h := 3 + 2*hot
		y := r.Y + (r.H-h)/2
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: h}, white.Alpha(0.22), h/2)
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W * at, H: h}, accent, h/2)
		if k := 13 * hot; k > 0.5 {
			pt.Fill(ui.Rect{X: r.X + r.W*at - k/2, Y: r.Y + (r.H-k)/2, W: k, H: k}, white, k/2)
		}
	})
}

// sidePanel shows the drama, its episodes and related dramas.
func (app *App) sidePanel(c *ui.Context, w *Watching) {
	p := app.pal
	ui.Column(c).Width(panelWidth).Shrink(0).Background(p.bg).BorderWidth(0, 0, 0, 1).BorderColor(p.hairline).Children(func() {
		ui.Box(c).Height(max(c.TitleBar().Height, 28) + 12).DragWindow()
		ui.Scroll(c.Key("side")).TrackScroll(&w.side).Grow(1).Padding(0, 20, 28).Gap(24).Children(func() {
			app.about(c, w)
			app.seasonRow(c, w)
			if w.detail != nil {
				app.episodes(c, w)
			} else if w.detailErr == "" {
				sh := app.shimmer(c)
				ui.Grid(c).Columns(6).Gap(8).Role(ui.RoleStatus).Label("加载中").Children(func() {
					for i := range 24 {
						sh.bone(c.Key(i), 9).Height(36)
					}
				})
			}
			app.related(c, w)
		})
	})
}

func (app *App) about(c *ui.Context, w *Watching) {
	p := app.pal
	ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
		app.poster(c, w.show.Cover, w.show.Title, 10).Width(96).Shrink(0)
		ui.Column(c).Grow(1).Gap(7).Children(func() {
			ui.Text(c, w.show.Title).FontSize(17).Bold().MaxLines(3).TextColor(p.text).Role(ui.RoleHeading).Level(2)
			var meta []string
			if w.detail != nil {
				meta = append(meta, fmt.Sprintf("全%d集", len(w.detail.Episodes)))
				if w.detail.Heat != "" {
					meta = append(meta, w.detail.Heat)
				}
			}
			if len(meta) > 0 {
				ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(p.muted)
			}
			if w.detail != nil {
				ui.Row(c).Gap(5).Wrap().Children(func() {
					shown := 0
					for _, tag := range w.detail.Tags {
						if shown >= 4 || strings.HasPrefix(tag, "全") && strings.HasSuffix(tag, "集") {
							continue
						}
						shown++
						ui.Text(c, tag).FontSize(11).TextColor(p.muted).Padding(2, 8).Radius(5).Background(p.surface)
					}
				})
			}
			following := app.lib.Following(w.show.ID)
			var b ui.Element
			if following {
				b = app.secondary(c, nil, "已追剧")
			} else {
				b = app.primary(c, iconHeart, "追剧")
			}
			b.Margin(4, 0, 0).AlignSelf(ui.Start)
			if b.Clicked() {
				app.toggleFollow(w.show)
			}
		})
	})
	if w.show.Desc != "" {
		// A button reads no text inside it: its Description reads the synopsis.
		d := ui.ButtonBase(c.Key("desc")).AlignItems(ui.Start).Justify(ui.Start).Shrink(1).Cursor(ui.CursorPointer).
			Label("简介").Description(w.show.Desc).Expanded(w.descOpen)
		d.Children(func() {
			t := ui.Text(c, w.show.Desc).FontSize(12.5).LineHeight(1.55).TextColor(p.muted).Grow(1).Shrink(1)
			if !w.descOpen {
				t.MaxLines(3)
			}
		})
		if d.Clicked() {
			w.descOpen = !w.descOpen
		}
	}
}

func (app *App) episodes(c *ui.Context, w *Watching) {
	p := app.pal
	eps := w.detail.Episodes
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "选集").FontSize(15).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			ui.Text(c, fmt.Sprintf("第%d集 / 共%d集", w.index+1, len(eps))).FontSize(12).TextColor(p.muted)
		})
		ranges := (len(eps) + episodesPerRange - 1) / episodesPerRange
		if ranges > 1 {
			ui.ScrollHorizontal(c.Key("ranges")).Children(func() {
				choices(c, "集数范围").Gap(6).Children(func() {
					for r := range ranges {
						from, to := r*episodesPerRange+1, min((r+1)*episodesPerRange, len(eps))
						chip := app.chip(c.Key(r), fmt.Sprintf("%d-%d", from, to), r == w.epRange).Shrink(0)
						if radio(chip, r == w.epRange).Label(fmt.Sprintf("第%d到%d集", from, to)).Clicked() {
							w.epRange = r
						}
					}
				})
			})
		}
		watched := -1
		if rec := app.lib.History[w.show.ID]; rec != nil {
			watched = rec.Episode
		}
		playing := w.player != nil && w.player.Playing()
		from := w.epRange * episodesPerRange
		ui.Grid(c.Key(w.epRange)).Columns(6).Gap(8).Children(func() {
			for i := from; i < min(from+episodesPerRange, len(eps)); i++ {
				ep := eps[i]
				current := i == w.index
				cell := ui.ButtonBase(c.Key(ep.ID)).Height(36).Radius(9).Center().Label(fmt.Sprintf("第%d集", i+1)).
					Description(ternary(!ep.Playable, "暂不可播放", ternary(i < watched, "已看过", "")))
				if current {
					cell.Current(ui.CurrentItem)
				}
				rest := ternary(current, p.accent, p.surface)
				pressable(cell, rest, ternary(current, rest, p.surface.Mix(p.text, 0.08)), 0.92)
				cell.Children(func() {
					if current && playing {
						sfIcon(c, "waveform").Size(13).Color(white)
						return
					}
					n := ui.Text(c, itoa(i+1)).FontSize(13).FontWeight(600)
					switch {
					case current:
						n.TextColor(white)
					case !ep.Playable:
						n.TextColor(p.faint)
					case i < watched:
						n.TextColor(p.muted)
					default:
						n.TextColor(p.text)
					}
					if !ep.Playable {
						sfIcon(c, "lock.fill").Size(7).Color(p.faint).Absolute().Top(4).Right(5)
					}
				})
				if cell.Clicked() && !current {
					w.drawer = false
					w.play(i, 0)
				}
			}
		})
	})
}

func (app *App) related(c *ui.Context, w *Watching) {
	p := app.pal
	if len(w.recs) == 0 {
		return
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, "相关推荐").FontSize(15).Bold().TextColor(p.text).Margin(0, 0, 8).Role(ui.RoleHeading).Level(2)
		for i, s := range w.recs {
			if i >= 12 {
				break
			}
			row := ui.ButtonBase(c.Key("rec-" + s.ID)).Gap(12).Padding(6).Radius(10).AlignItems(ui.Start).Label(s.Title).
				Description(joinNonEmpty(ternary(s.Episodes > 0, fmt.Sprintf("全%d集", s.Episodes), ""), s.Desc))
			pressable(row, ui.Transparent, p.hover, 0.98)
			row.Children(func() {
				app.poster(c, s.Cover, s.Title, 7).Width(56).Shrink(0)
				ui.Column(c).Grow(1).Gap(4).Padding(2, 0).Children(func() {
					ui.Text(c, s.Title).FontSize(13).FontWeight(600).MaxLines(2).TextColor(p.text)
					if s.Episodes > 0 {
						ui.Text(c, fmt.Sprintf("全%d集", s.Episodes)).FontSize(11.5).TextColor(p.muted)
					}
					if s.Desc != "" {
						ui.Text(c, s.Desc).FontSize(11.5).TextColor(p.faint).MaxLines(2)
					}
				})
			})
			if row.Clicked() {
				app.open(s, -1)
			}
		}
	})
}

// seasonLine says where a season stands: when it comes out, that it is
// new, how many wait for it.
func seasonLine(s Season) string {
	parts := []string{fmt.Sprintf("第%d季", s.Index)}
	switch {
	case !s.Online:
		parts = append(parts, ternary(s.Note != "", s.Note, "即将上线"))
	case s.New:
		parts = append(parts, ternary(s.Note != "", s.Note, "新上线"))
	}
	if s.Heat != "" {
		parts = append(parts, s.Heat)
	}
	return strings.Join(parts, " · ")
}

// seasonRow switches between the seasons of a drama that has several.
func (app *App) seasonRow(c *ui.Context, w *Watching) {
	p := app.pal
	if seasonOf(w.seasons, w.show.ID) < 0 {
		return
	}
	latest := w.seasons[len(w.seasons)-1]
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "系列").FontSize(15).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			ui.Text(c, fmt.Sprintf("共%d季", len(w.seasons))).FontSize(12).TextColor(p.muted)
		})
		ui.ScrollHorizontal(c.Key("seasons")).Gap(6).Children(func() {
			for _, s := range w.seasons {
				label := fmt.Sprintf("第%d季", s.Index)
				switch {
				case !s.Online:
					label += " · 预告"
				case s.New:
					label += " · 新"
				}
				current := s.ID == w.show.ID
				chip := app.chip(c.Key("season-"+s.ID), label, current).Shrink(0).Label(label)
				if current {
					chip.Current(ui.CurrentItem)
				}
				if !chip.Clicked() || current {
					continue
				}
				if s.Online {
					w.switchSeason(s, -1)
				} else {
					app.toast = seasonLine(s)
				}
			}
		})
		if latest.New || !latest.Online {
			ui.Text(c, seasonLine(latest)).FontSize(12).TextColor(p.muted)
		}
	})
}
