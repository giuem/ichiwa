package app

import (
	"context"
	"fmt"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

// SpeedSteps are the speeds to choose from, in order, for the panel and
// for the keys that step them.
var SpeedSteps = []float64{0.75, 1, 1.25, 1.5, 2}

// BoostRate is the speed while the right arrow is held, and boostDelay how
// long it takes being held rather than pressed.
const (
	BoostRate  = 2
	boostDelay = 350 * time.Millisecond
)

// ClickDelay is how long a click on the picture waits to see whether it is
// the first of a double click, and clickUndo how late that can turn out,
// as a slow double click goes by the system's longer interval.
const (
	ClickDelay = 220 * time.Millisecond
	clickUndo  = 600 * time.Millisecond
)

// SleepEpisode is the sleep timer's choice of stopping as the episode ends.
const SleepEpisode = -1

// The smallest the small window gets, and the long side it starts at.
const (
	MiniMinWidth  = 340
	MiniMinHeight = 360
	miniLongSide  = 640
)

// maxSkips is how many episodes in a row autoplay skips when unavailable
// before it gives up and says so.
const maxSkips = 3

// EpisodesPerRange is how many episodes the episode grid shows at once.
const EpisodesPerRange = 50

// PanelWidth is the width of the player page's side panel. Pinning the
// window takes it off the window.
const PanelWidth = 368

// Watching is the player page: a drama, the episode playing and the
// player playing it.
type Watching struct {
	App      *App
	SourceID string
	Src      source.Source // nil when the source is not loaded
	Show     Card

	Detail    *Media
	DetailErr string
	// Related is the sections under the details, their cards filled in
	// as they load.
	Related []Section

	Index         int
	gen           int // counts episodes started, to drop stale results
	Player        Player
	stop          chan struct{}
	preloadCancel context.CancelFunc
	// ctx ends when the page is left, playCtx when the episode is: the
	// source's calls for them end with them (§9).
	ctx        context.Context
	cancel     context.CancelFunc
	playCancel context.CancelFunc
	Loading    bool
	Err        string
	Ended      bool
	Paused     bool // the user paused
	auto       bool // the next play call comes from autoplay
	Skipped    int  // episodes autoplay skipped in a row
	RetryAt    time.Time
	awake      func() // releases the display, while it is kept on

	Seek     float64
	Dragging bool
	Panel    bool
	Unpin    bool // the panel opens again when the window is unpinned
	DescOpen bool
	EpRange  int

	// Where the pointer was over the stage in full screen, and since when.
	pointerX, pointerY float32
	pointerAt          time.Time

	// The right arrow: when it went down, while it is, and whether holding
	// it sped the video up, which then asks for no seek as it comes up.
	RightAt        time.Time
	Boost, boosted bool

	KeyAt time.Time // when a key last acted on the video

	// A click on the picture waiting to count, when one last did, and
	// whether the release of a double click is still to come.
	ClickAt, toggledAt time.Time
	skipClick          bool

	Halt   bool // the episode ended and the page waits rather than go on
	Drawer bool // the episodes are open over the picture

	// UI is the view's own state of the page, which the app never reads.
	UI        any
	DM        *Danmaku // the comments of the episode playing
	DmOpen    bool     // the danmaku settings show
	SpeedOpen bool     // the speed picker shows
	VolOpen   bool     // the volume panel shows
	Told      string   // what screen readers were last told is playing

	// seasons are the drama's seasons when it has more than one, the one
	// showing among them; switchSeason opens another at an episode, -1
	// for where the user left it.
	Seasons      []Season
	SwitchSeason func(s Season, ep int)
}

// has tells whether the drama's source has capability c.
func (w *Watching) Has(c source.Capability) bool {
	return w.Src != nil && w.Src.Info().Has(c)
}

func (w *Watching) Ref() source.Ref {
	srcID := w.SourceID
	if srcID == "" && w.App != nil {
		srcID = w.App.SourceID()
	}
	return source.Ref{Source: srcID, ID: w.Show.ID}
}

func SeasonCard(next Season) Card {
	title := next.Name
	if title == "" {
		title = next.Title
	}
	return Card{
		Kind:  source.CardMedia,
		ID:    next.ID,
		Title: title,
		Image: next.Image,
		Shape: source.ShapePortrait,
	}
}

func episodeIDs(eps []source.Episode) []string {
	ids := make([]string, len(eps))
	for i, ep := range eps {
		ids[i] = ep.ID
	}
	return ids
}

func episodeIndex(eps []source.Episode, id string) int {
	for i, ep := range eps {
		if ep.ID == id {
			return i
		}
	}
	return -1
}

func LockMessage(lock source.Lock) string {
	switch lock {
	case source.LockLogin:
		return "该集需要登录才能观看"
	case source.LockVIP:
		return "该集需要会员才能观看"
	case source.LockPaid:
		return "该集需要付费才能观看"
	default:
		return "该集暂不可播放"
	}
}

// open shows the player page for a drama, at episode ep, or where the
// user left it when ep is -1.
// A record of a source not shown opens only while the source is loaded
// and on; otherwise Unavailable says so.
func (app *App) Open(sourceID string, s Card, ep int) {
	if sourceID == "" {
		sourceID = app.SourceID()
	}
	if !app.Available(sourceID) {
		app.Unavailable = &Unavailable{Source: sourceID, Name: app.SourceName(sourceID), Title: s.Title,
			Installed: app.Sources.Get(sourceID) != nil}
		return
	}
	app.openIn(sourceID, s, ep, nil)
}

// Unavailable is a record whose source is not available, asked about.
type Unavailable struct {
	Source    string // its ID
	Name      string // its name, or its ID when it is not installed
	Title     string // the record's
	Installed bool   // it is installed and turned off
}

// Available tells the source id can open what it gave: it is loaded and on.
func (app *App) Available(id string) bool {
	return app.Sources.Get(id) != nil && app.SourceOn(id)
}

// SourceName is the name of the source id, its ID when it is not loaded.
func (app *App) SourceName(id string) string {
	if s := app.Sources.Get(id); s != nil {
		return s.Info().Name
	}
	return id
}

// openIn opens a drama among seasons already known, which the page keeps
// until Media returns.
func (app *App) openIn(sourceID string, s Card, ep int, seasons []Season) {
	if sourceID == "" {
		sourceID = app.SourceID()
	}
	var awake func()
	if app.Watch != nil {
		app.Watch.Close()
		// A season going on to the next keeps the display on throughout.
		awake, app.Watch.awake = app.Watch.awake, nil
	}
	w := &Watching{App: app, SourceID: sourceID, Show: s, Index: max(ep, 0), Loading: true, Panel: !app.Lib.OnTop, Unpin: app.Lib.OnTop, awake: awake}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.SwitchSeason = func(next Season, ep int) { app.openIn(w.SourceID, SeasonCard(next), ep, w.Seasons) }
	if SeasonOf(seasons, s.ID) >= 0 {
		w.Seasons = seasons
	}
	app.Watch = w
	if app.host != nil {
		go w.keepAwake()
	}

	src := app.Sources.Get(sourceID)
	w.Src = src
	if src == nil {
		w.Loading = false
		w.DetailErr = "片源不可用：这部剧来自没有加载的片源"
		return
	}
	ctx := w.ctx
	async(app, func() (*Media, error) { return src.Media(ctx, s.ID) }, func(d *Media, err error) {
		if app.Watch != w || ctx.Err() != nil {
			return
		}
		if err != nil {
			w.Loading = false
			w.DetailErr = errText(err)
			return
		}
		w.Detail = d
		// Keep what the grid knew when the detail lacks it.
		img := d.Image
		if img == "" {
			img = s.Image
		}
		w.Show = Card{
			Kind:     source.CardMedia,
			ID:       d.ID,
			Title:    d.Title,
			Subtitle: d.Description,
			Image:    img,
			Shape:    source.ShapePortrait,
			Badges:   d.Badges,
			Note:     s.Note,
		}
		if len(d.Seasons) > 0 && SeasonOf(d.Seasons, d.ID) >= 0 {
			w.Seasons = d.Seasons
		}
		app.Lib.ResolveEpisodes(w.Ref(), episodeIDs(d.Episodes))

		start, at := ep, 0.0
		if ep < 0 {
			start = 0
			if rec := app.Lib.History[w.Ref()]; rec != nil {
				if rec.EpisodeID != "" {
					if idx := episodeIndex(d.Episodes, rec.EpisodeID); idx >= 0 {
						start = idx
					} else {
						start = rec.Episode
					}
				} else {
					start = rec.Episode
				}
				at = rec.Position
				if rec.Duration > 0 && at > rec.Duration-3 { // finished: the next one
					start, at = start+1, 0
				}
			}
		}
		start = min(max(start, 0), len(d.Episodes)-1)
		w.Play(start, at)

		// The sections under the details: related dramas and the like,
		// loaded as they name their collections.
		w.Related = nil
		for _, sec := range d.Sections {
			if len(sec.Items) == 0 && sec.Collection == "" {
				continue
			}
			i := len(w.Related)
			w.Related = append(w.Related, sec)
			if len(sec.Items) > 0 {
				continue
			}
			colID := sec.Collection
			async(app, func() (*source.CollectionPage, error) {
				return src.Collection(ctx, source.CollectionRequest{ID: colID})
			}, func(p *source.CollectionPage, err error) {
				if app.Watch == w && ctx.Err() == nil && err == nil && p != nil {
					w.Related[i].Items = p.Items
				}
			})
		}
	})
}

func (app *App) CloseWatch() {
	if app.Watch != nil {
		app.MiniOn = false
		app.SetSleep(0)
		if app.host != nil { // only the player page stays on top
			app.host.SetAlwaysOnTop(false)
		}
		app.Watch.Close()
		app.Watch = nil
		if app.host != nil {
			app.host.NowPlaying(nil)
		}
	}
}

// context is the context of the page's calls.
func (w *Watching) context() context.Context {
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	return w.ctx
}

func (w *Watching) stopPlayer() {
	w.dropDanmaku()
	if w.playCancel != nil {
		w.playCancel()
		w.playCancel = nil
	}
	if w.preloadCancel != nil {
		w.preloadCancel()
		w.preloadCancel = nil
	}
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
	if w.Player != nil {
		w.SaveProgress()
		w.Player.Close()
		w.Player = nil
	}
}

// close leaves the page.
func (w *Watching) Close() {
	w.stopPlayer()
	if w.cancel != nil {
		w.cancel()
	}
	w.gen++
	w.App.Lib.Flush()
}

func (w *Watching) SaveProgress() {
	if w.Player == nil || w.Detail == nil || w.Index < 0 || w.Index >= len(w.Detail.Episodes) {
		return
	}
	ep := w.Detail.Episodes[w.Index]
	w.App.Lib.Progress(w.Ref().Source, w.Show, ep.ID, w.Index, len(w.Detail.Episodes), w.Player.Position().Seconds(), w.Player.Duration().Seconds())
}

// play starts episode i at second at.
func (w *Watching) Play(i int, at float64) {
	app := w.App
	w.stopPlayer()
	w.gen++
	gen := w.gen
	auto := w.auto
	w.auto = false
	if !auto {
		w.Skipped = 0
	}
	w.Index, w.Err, w.Ended, w.Paused, w.Loading = i, "", false, false, true
	w.Halt = false
	w.RetryAt = time.Time{}
	w.Boost = false
	w.EpRange = i / EpisodesPerRange
	ep := w.Detail.Episodes[i]
	if ep.Lock != source.LockNone {
		if auto && i+1 < len(w.Detail.Episodes) {
			w.auto = true
			w.Play(i+1, 0)
			return
		}
		w.Loading, w.Err = false, LockMessage(ep.Lock)
		return
	}
	ref := w.Ref()
	app.Lib.Progress(ref.Source, w.Show, ep.ID, i, len(w.Detail.Episodes), at, 0)
	src, mgr := w.Src, app.Media
	if src == nil || mgr == nil {
		return
	}
	playCtx, playCancel := context.WithCancel(w.context())
	w.playCancel = playCancel
	go func() {
		item, err := mgr.Open(playCtx, ref.Source, ref.ID, ep.ID, func(ctx context.Context, prefer source.PlayPrefer) (*source.Playback, error) {
			return src.Play(ctx, source.PlayRequest{MediaID: ref.ID, EpisodeID: ep.ID, Prefer: prefer})
		})
		app.dispatch(func() {
			if app.Watch != w || w.gen != gen || playCtx.Err() != nil {
				return
			}
			w.Loading = false
			var p Player
			if err == nil {
				p, err = app.host.OpenVideo(item.URL, item.Path)
			}
			if err != nil {
				w.failed(i, auto, err)
				return
			}
			w.Skipped = 0
			w.Player = p
			// It starts playing once the view shows it.
			p.SetVolume(app.Lib.Volume)
			p.SetRate(app.Lib.Speed)
			if at > 1 {
				p.Seek(Seconds(at - 1))
			}
			w.stop = make(chan struct{})
			go w.watchEnd(gen, p, w.stop)
			// Control Center learns the duration once the item loaded.
			time.AfterFunc(600*time.Millisecond, func() {
				w.App.sync(func() {
					if w.gen == gen && app.Watch == w {
						w.nowPlaying()
					}
				})
			})
			// The next episode downloads meanwhile, so it starts at once.
			if i+1 < len(w.Detail.Episodes) && w.Detail.Episodes[i+1].Lock == source.LockNone {
				nextID := w.Detail.Episodes[i+1].ID
				pCtx, pCancel := context.WithCancel(w.context())
				w.preloadCancel = pCancel
				mgr.Preload(pCtx, ref.Source, ref.ID, nextID, 2*time.Second, func(ctx context.Context, prefer source.PlayPrefer) (*source.Playback, error) {
					return src.Play(ctx, source.PlayRequest{MediaID: ref.ID, EpisodeID: nextID, Prefer: prefer})
				})
			}
		})
	}()
}

// watchEnd notices the end of the episode. It asks the clock rather than
// wait for the view, as a window out of sight builds no frames while the
// sound plays on.
func (w *Watching) watchEnd(gen int, p Player, stop chan struct{}) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		var failed, finished bool
		w.App.sync(func() {
			if w.gen != gen || w.App.Watch != w { // the player may be closed
				return
			}
			dur := p.Duration()
			failed = p.Err() != nil
			finished = p.Ended() || dur > 0 && p.Position() >= dur-300*time.Millisecond
		})
		if !failed && !finished {
			continue
		}
		w.App.dispatch(func() {
			if w.gen != gen || w.App.Watch != w {
				return
			}
			if failed {
				w.Err = "播放失败"
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
func (w *Watching) WantsDisplay() bool {
	return w.App.Watch == w && w.Err == "" && !w.Ended && !w.Halt && !w.Paused && (w.Loading || w.Player != nil)
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
		w.App.sync(func() {
			if w.sleepTick(time.Now()) {
				w.App.redraw()
			}
			switch want := w.WantsDisplay(); {
			case want && w.awake == nil:
				w.awake = w.App.host.KeepAwake("正在播放短剧")
			case !want && w.awake != nil:
				w.awake()
				w.awake = nil
			}
			left = w.App.Watch != w
		})
		if left {
			return
		}
		<-tick.C
	}
}

// failed shows why episode i did not play. Autoplay skips an unavailable
// episode, up to maxSkips in a row.
func (w *Watching) failed(i int, auto bool, err error) {
	app := w.App
	if auto && source.Is(err, source.ErrUnavailable) && i+1 < len(w.Detail.Episodes) {
		w.Skipped++
		if w.Skipped < maxSkips {
			app.Toast = fmt.Sprintf("第%d集暂时无法播放，已跳过", i+1)
			w.auto = true
			w.Play(i+1, 0)
			return
		}
	}
	w.Err = errText(err)
	if w.Skipped >= maxSkips {
		w.Err = fmt.Sprintf("连续%d集无法播放：%s", w.Skipped, w.Err)
	}
	if wait := source.RetryAfter(err); wait > 0 {
		w.RetryAt = time.Now().Add(wait)
		until := w.RetryAt
		go func() { // redraw the countdown each second
			for time.Now().Before(until) {
				time.Sleep(time.Second)
				app.redraw()
			}
		}()
	}
}

// retryWait is how many Seconds the retry button still waits.
func (w *Watching) RetryWait() int {
	if d := time.Until(w.RetryAt); d > 0 {
		return int(d.Seconds()) + 1
	}
	return 0
}

func SeasonOf(seasons []Season, id string) int {
	for i, s := range seasons {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// nextSeason is the season after the one showing, if there is one.
func (w *Watching) NextSeason() (Season, bool) {
	i := SeasonOf(w.Seasons, w.Show.ID)
	if i < 0 || i+1 >= len(w.Seasons) {
		return Season{}, false
	}
	return w.Seasons[i+1], true
}

// lastEpisode tells whether the episode playing is the season's last.
func (w *Watching) LastEpisode() bool {
	return w.Detail != nil && w.Index+1 >= len(w.Detail.Episodes)
}

// toNextSeason starts the next season from its first episode, if it is
// out, and tells whether it did.
func (w *Watching) toNextSeason() bool {
	next, ok := w.NextSeason()
	if !ok || !next.IsAvailable() {
		return false
	}
	w.App.Toast = "接着看" + next.Title
	w.SwitchSeason(next, 0)
	return true
}

// hasNext tells whether something follows the episode playing.
func (w *Watching) HasNext() bool {
	if !w.LastEpisode() {
		return true
	}
	next, ok := w.NextSeason()
	return ok && next.IsAvailable()
}

// SleepOptions are the sleep timer's choices, in minutes.
var SleepOptions = []struct {
	Label string
	Mins  int
}{{"不开启", 0}, {"播完本集", SleepEpisode}, {"15 分钟", 15}, {"30 分钟", 30}, {"60 分钟", 60}}

// setSleep sets the sleep timer to one of SleepOptions.
func (app *App) SetSleep(mins int) {
	app.SleepMins, app.SleepAt = mins, time.Time{}
	if mins > 0 {
		app.SleepAt = time.Now().Add(time.Duration(mins) * time.Minute)
	}
}

// sleepTick pauses the video once the sleep timer ran out, and tells
// whether it did anything. It asks the clock, as a window out of sight
// builds no views. An episode still loading waits for its player.
func (w *Watching) sleepTick(now time.Time) bool {
	app := w.App
	if app.SleepAt.IsZero() || now.Before(app.SleepAt) || w.Loading {
		return false
	}
	app.SetSleep(0)
	if w.Player != nil && w.Player.Playing() {
		w.TogglePlay()
		app.Toast = "定时已到，已暂停"
	}
	return true
}

func (w *Watching) ToggleAutoplay() {
	lib := w.App.Lib
	lib.Autoplay = !lib.Autoplay
	lib.MarkDirty()
	w.App.Toast = ternary(lib.Autoplay, "已开启自动连播", "已关闭自动连播")
}

// pictureClicks turns what the picture got this frame into what to do: a
// click waits out ClickDelay, so that a double click goes to full screen
// without pausing, and one slower than that puts the pause back.
func (w *Watching) PictureClicks(clicked, double, pressed bool, now time.Time) (toggle, full bool) {
	if double {
		full, w.skipClick = true, true
		if w.ClickAt.IsZero() && !w.toggledAt.IsZero() && now.Sub(w.toggledAt) < clickUndo {
			toggle = true
		}
		w.ClickAt, w.toggledAt = time.Time{}, time.Time{}
	}
	switch {
	case clicked && w.skipClick:
		w.skipClick = false
	case clicked:
		w.ClickAt = now
	case !pressed:
		w.skipClick = false // the second press let go elsewhere
	}
	if !w.ClickAt.IsZero() && now.Sub(w.ClickAt) >= ClickDelay {
		w.ClickAt, w.toggledAt, toggle = time.Time{}, now, true
	}
	return toggle, full
}

func (w *Watching) onEnded() {
	if w.Player != nil {
		w.SaveProgress()
	}
	app := w.App
	once := app.SleepMins == SleepEpisode
	if once {
		app.SetSleep(0)
	}
	if w.HasNext() && (once || !app.Lib.Autoplay) {
		w.Halt = true
		if p := w.Player; p != nil {
			p.Pause()
			w.SyncNowPlaying()
		}
		return
	}
	if w.Index+1 < len(w.Detail.Episodes) {
		w.auto = true
		w.Play(w.Index+1, 0)
		return
	}
	if w.toNextSeason() {
		return
	}
	w.Ended = true
}

func (w *Watching) TogglePlay() {
	p := w.Player
	if p == nil {
		return
	}
	if w.Ended || w.Halt { // the watch on its end is over: start it anew
		w.Play(w.Index, 0)
		return
	}
	if p.Playing() {
		p.Pause()
		w.Paused = true
		w.SaveProgress()
	} else {
		p.Play()
		w.Paused = false
	}
	w.SyncNowPlaying()
}

// toggleOnTop pins the window above others, or unpins it. A pinned window
// is out of the way as far as it can be, so the panel folds, and opens
// again on unpinning unless the user touched it since.
func (w *Watching) ToggleOnTop() {
	lib := w.App.Lib
	lib.OnTop = !lib.OnTop
	lib.MarkDirty()
	switch {
	case lib.OnTop && w.Panel:
		w.Panel, w.Unpin = false, true
	case !lib.OnTop && w.Unpin:
		w.Panel, w.Unpin = true, false
	}
	w.App.Toast = ternary(lib.OnTop, "窗口已置顶", "已取消置顶")
}

// toggleMini asks for the small window that fits the video, or for the
// window it was.
func (w *Watching) ToggleMini() {
	app := w.App
	app.MiniOn = !app.MiniOn
	app.Toast = ternary(app.MiniOn, "已进入小窗", "已退出小窗")
}

// MiniSize is the small window for a video of this size, portrait when
// the size is not known yet.
func MiniSize(vw, vh int) (int, int) {
	if vw <= 0 || vh <= 0 {
		vw, vh = 9, 16
	}
	if vh >= vw {
		return max(miniLongSide*vw/vh, MiniMinWidth), miniLongSide
	}
	return miniLongSide, max(miniLongSide*vh/vw, MiniMinHeight)
}

// DrawerFits tells the episodes open over the picture here: full screen
// and a window with no room for the side panel have nowhere else for them.
func DrawerFits(width float32, full bool) bool { return full || !PanelFits(width) }

// toggleEpisodes opens or closes the episodes, wherever they go here.
func (w *Watching) ToggleEpisodes(width float32, full bool) {
	if DrawerFits(width, full) {
		w.Drawer = !w.Drawer
	} else {
		w.Panel, w.Unpin = !w.Panel, false
	}
}

// Narrowing is how much of the panel's width a window this wide can give up.
func Narrowing(width int) int {
	return max(min(PanelWidth, width-MinWindowWidth), 0)
}

// PanelFits tells a window this wide has room for the side panel beside
// the controls of the video.
func PanelFits(width float32) bool { return width >= MinWindowWidth+PanelWidth }

func (w *Watching) Step(d int) {
	if w.Detail == nil {
		return
	}
	i := w.Index + d
	if i >= 0 && i < len(w.Detail.Episodes) {
		w.Play(i, w.App.Lib.EpisodeStart(w.Ref(), w.Detail.Episodes[i].ID))
	} else if d > 0 {
		w.toNextSeason()
	}
}

func (w *Watching) SeekBy(d float64) {
	if p := w.Player; p != nil {
		p.Seek(min(max(p.Position()+Seconds(d), 0), p.Duration()))
		w.KeyAt = time.Now()
		w.SyncNowPlaying()
	}
}

// holdRight follows the right arrow as it is held: a press seeks as it
// comes up, a hold plays faster until it does.
func (w *Watching) HoldRight(down bool, now time.Time) {
	switch {
	case w.RightAt.IsZero():
	case !down:
		if w.Boost && w.Player != nil {
			w.Player.SetRate(w.App.Lib.Speed)
		}
		if !w.boosted {
			w.SeekBy(5)
		}
		w.RightAt, w.Boost, w.boosted = time.Time{}, false, false
	case !w.Boost && now.Sub(w.RightAt) >= boostDelay && w.Player != nil && w.Player.Playing():
		w.Player.SetRate(BoostRate)
		w.Boost, w.boosted = true, true
	}
}

// resumeAt is where a retry of the episode picks up.
func (w *Watching) ResumeAt() float64 {
	if w.Player != nil {
		return w.Player.Position().Seconds()
	}
	if w.Detail != nil && w.Index >= 0 && w.Index < len(w.Detail.Episodes) {
		return w.App.Lib.EpisodeStart(w.Ref(), w.Detail.Episodes[w.Index].ID)
	}
	return 0
}

func (w *Watching) SetVolume(v float64) {
	lib := w.App.Lib
	lib.Volume = min(max(v, 0), 1)
	lib.MarkDirty()
	if p := w.Player; p != nil {
		p.SetVolume(lib.Volume)
	}
	w.KeyAt = time.Now()
}

// toggleMute silences the video, or brings back the volume it had.
func (w *Watching) ToggleMute() {
	app := w.App
	if app.Lib.Volume > 0 {
		app.unmute = app.Lib.Volume
		w.SetVolume(0)
		app.Toast = "已静音"
		return
	}
	w.SetVolume(ternary(app.unmute > 0, app.unmute, 1.0))
	app.Toast = volumeText(app.Lib.Volume)
}

func (w *Watching) NudgeVolume(d float64) {
	w.SetVolume(w.App.Lib.Volume + d)
	w.App.Toast = volumeText(w.App.Lib.Volume)
}

func volumeText(v float64) string { return fmt.Sprintf("音量 %d%%", int(v*100+0.5)) }

func (w *Watching) SetSpeed(next float64) {
	app := w.App
	app.Lib.Speed = next
	app.Lib.MarkDirty()
	if p := w.Player; p != nil && !w.Boost {
		p.SetRate(next)
	}
	w.KeyAt = time.Now()
	app.Toast = fmt.Sprintf("播放速度 %gx", next)
}

// stepSpeed moves the speed d steps along SpeedSteps, stopping at its ends.
func (w *Watching) StepSpeed(d int) { w.SetSpeed(SteppedSpeed(w.App.Lib.Speed, d)) }

func SteppedSpeed(cur float64, d int) float64 {
	for i, s := range SpeedSteps {
		if s == cur {
			return SpeedSteps[min(max(i+d, 0), len(SpeedSteps)-1)]
		}
	}
	return 1
}

// Seconds is s Seconds as a Duration.
func Seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// news is what screen readers are told as it changes: the episode that
// started, and the end. Failures are read as the alerts they show as.
func (w *Watching) News() string {
	switch {
	case w.Detail == nil || w.Loading || w.Err != "":
		return ""
	case w.Ended:
		return w.Show.Title + " 已看完"
	case w.Halt:
		return fmt.Sprintf("第%d集播完", w.Index+1)
	}
	return fmt.Sprintf("正在播放 %s 第%d集", w.Show.Title, w.Index+1)
}

// pointerStill is how long the pointer has rested on the stage, which only
// counts while the video plays and the user is not seeking. The frames that
// let the controls go are those playing asks for anyway.
func (w *Watching) PointerStill(x, y float32, playing bool) time.Duration {
	now := time.Now()
	if x != w.pointerX || y != w.pointerY || !playing || w.Dragging {
		w.pointerX, w.pointerY, w.pointerAt = x, y, now
	}
	return now.Sub(w.pointerAt)
}
