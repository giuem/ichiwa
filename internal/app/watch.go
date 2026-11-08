package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	"hongguo/internal/catalog"
)

var speeds = []float64{1, 1.25, 1.5, 2, 0.75}

// maxSkips is how many episodes in a row autoplay skips because the
// anonymous device cannot play them before it gives up and says so.
const maxSkips = 3

// retryCooldown is how long the retry button waits after the server
// limited anonymous playback: the SDK already tried fresh devices, and
// retrying at once only registers more of them.
const retryCooldown = 30 * time.Second

// episodesPerRange is how many episodes the episode grid shows at once.
const episodesPerRange = 50

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

	seek     float64
	dragging bool
	panel    bool
	descOpen bool
	epRange  int
	side     ui.ScrollState

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
	if app.watch != nil {
		app.watch.close()
	}
	w := &Watching{app: app, show: s, index: max(ep, 0), loading: true, panel: true}
	w.switchSeason = func(next Season, ep int) { app.openIn(next.Show, ep, w.seasons) }
	app.watch = w
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
		app.watch.close()
		app.watch = nil
		mygo.NowPlaying.Clear()
	}
}

func (w *Watching) stopPlayer() {
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
	w.retryAt = time.Time{}
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

func (w *Watching) onEnded() {
	if w.player != nil {
		w.saveProgress()
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
	if p.Playing() && !w.ended {
		p.Pause()
		w.paused = true
		w.saveProgress()
	} else {
		if w.ended {
			w.ended = false
			p.Seek(0)
		}
		p.Play()
		w.paused = false
	}
	w.syncNowPlaying()
}

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
		w.syncNowPlaying()
	}
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
	c.Root().Background(app.pal.bg)
	full := app.win.IsFullScreen()

	// Keyboard.
	c.OnShortcut(0, ui.KeySpace, w.togglePlay)
	c.OnShortcut(0, ui.KeyLeft, func() { w.seekBy(-5) })
	c.OnShortcut(0, ui.KeyRight, func() { w.seekBy(5) })
	c.OnShortcut(0, ui.KeyUp, func() { w.step(-1) })
	c.OnShortcut(0, ui.KeyDown, func() { w.step(1) })
	c.OnShortcut(0, ui.KeyF, app.win.ToggleFullScreen)
	c.OnShortcut(0, ui.KeyEscape, func() {
		if app.win.IsFullScreen() {
			app.win.SetFullScreen(false)
			return
		}
		app.closeWatch()
	})

	playing := w.player != nil && w.player.Playing()
	if w.player != nil {
		if playing {
			c.After(250 * time.Millisecond) // the clock and the progress bar
		}
		if !w.dragging {
			w.saveProgress()
		}
	}

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		app.stage(c, w, playing, full)
		if w.panel && !full {
			app.sidePanel(c, w)
		}
	})
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
			sfsymbols.Symbol(c, icon.name).Size(size).Color(white).Absolute().Top(0).Left(0).Scale(s, s).Opacity(min(icon.shown, 1))
		}
	})
}

// stage is the video with its title bar and controls.
func (app *App) stage(c *ui.Context, w *Watching, playing, full bool) {
	bar := c.TitleBar()
	stage := ui.Column(c).Grow(1).Background(black)
	hover := stage.Hovered()
	show := float32(1)
	if playing && !hover {
		show = 0
	}
	show = stage.AnimateTo("chrome", show, ui.Smooth)
	stage.Children(func() {
		// The picture, under everything.
		// Under the title bar and the controls, which are in the flow: absolute
		// elements otherwise paint over those and take their clicks.
		video := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().ZIndex(-1)
		if video.Clicked() {
			w.togglePlay()
		}
		video.Children(func() {
			// Built before its first picture too: only then does it start.
			if w.player != nil {
				ui.Video(c, w.player).Fill().Fit(ui.Contain)
			}
			if w.player == nil || !w.player.Ready() {
				if bmp := app.posters.Get(w.show.Cover); bmp != nil {
					ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Opacity(0.3).Blur(40)
				}
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(14).PassThrough().Children(func() {
				app.stageMessage(c, w)
			})
		})

		// Title bar.
		ui.Row(c).Padding(max(bar.Height, 28)/2-2, 14, 34, bar.Left+14).Gap(8).DragWindow().
			Gradient(ui.RGBA(0, 0, 0, 0.7), ui.Transparent, 180).Opacity(max(show, boolf(!playing))).
			Children(func() {
				if symbolButton(c, "chevron.left", 15, white, "返回 (Esc)").Clicked() {
					app.closeWatch()
				}
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, w.show.Title).FontSize(15).Bold().TextColor(white).SingleLine()
					if w.detail != nil {
						ui.Text(c, fmt.Sprintf("第%d集 · 共%d集", w.index+1, len(w.detail.Episodes))).FontSize(11.5).TextColor(white.Alpha(0.65))
					}
				})
				if !full && symbolButton(c, "sidebar.right", 15, white, "选集面板").Clicked() {
					w.panel = !w.panel
				}
			})

		ui.Spacer(c)

		// Controls.
		ui.Column(c).Padding(40, 18, 12).Gap(4).Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.78), 180).Opacity(show).Children(func() {
			app.progress(c, w)
			ui.Row(c).Gap(2).Children(func() {
				hasPrev := w.detail != nil && w.index > 0
				next, hasNextSeason := w.nextSeason()
				hasNextSeason = hasNextSeason && next.Online && w.lastEpisode()
				hasNext := w.detail != nil && w.index+1 < len(w.detail.Episodes) || hasNextSeason
				if symbolButton(c, "backward.end.fill", 15, white, "上一集 (↑)").Disabled(!hasPrev).Clicked() {
					w.step(-1)
				}
				pb := roundButton(c, 22, white, "播放/暂停 (空格)")
				pb.Children(func() { playPause(c, pb, playing, 22) })
				if pb.Clicked() {
					w.togglePlay()
				}
				if symbolButton(c, "forward.end.fill", 15, white, ternary(hasNextSeason, "下一季 (↓)", "下一集 (↓)")).Disabled(!hasNext).Clicked() {
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
				app.volume(c, w)
				fs := ternary(full, "arrow.down.right.and.arrow.up.left", "arrow.up.left.and.arrow.down.right")
				if symbolButton(c, fs, 14, white, "全屏 (F)").Clicked() {
					app.win.ToggleFullScreen()
				}
			})
		})
	})
}

// stageMessage is what shows over the picture: loading, errors, the end.
func (app *App) stageMessage(c *ui.Context, w *Watching) {
	dim := white.Alpha(0.7)
	switch {
	case w.detailErr != "":
		sfsymbols.Symbol(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.detailErr).FontSize(15).TextColor(white)
		app.secondary(c, nil, "返回").OnClick(app.closeWatch)
	case w.err != "":
		sfsymbols.Symbol(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.err).FontSize(15).TextColor(white)
		ui.Row(c).Gap(10).Margin(6, 0, 0).Children(func() {
			label := "重试"
			wait := w.retryWait()
			if wait > 0 {
				label = fmt.Sprintf("重试 (%ds)", wait)
			}
			app.secondary(c, iconRefresh, label).Disabled(wait > 0).OnClick(func() {
				if w.retryWait() == 0 {
					w.play(w.index, 0)
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
			sfsymbols.Symbol(c, "play.fill").Size(30).Color(white).Margin(0, 0, 0, 4)
		})
	}
}

func (app *App) speedButton(c *ui.Context, w *Watching) {
	speed := app.lib.Speed
	b := ui.ButtonBase(c.Key("speed")).Height(28).PaddingX(10).Radius(999).Center().FocusRing(false).Label("播放速度").Tooltip("播放速度")
	pressable(b, ui.Transparent, white.Alpha(0.14), 0.92)
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%gx", speed)).FontSize(12.5).Bold().TextColor(white)
	})
	if b.Clicked() {
		next := speeds[0]
		for i, s := range speeds {
			if s == speed {
				next = speeds[(i+1)%len(speeds)]
			}
		}
		app.lib.Speed = next
		app.lib.MarkDirty()
		if p := w.player; p != nil {
			p.SetRate(next)
		}
		app.toast = fmt.Sprintf("播放速度 %gx", next)
	}
}

func (app *App) volume(c *ui.Context, w *Watching) {
	vol := app.lib.Volume
	icon := "speaker.wave.2.fill"
	switch {
	case vol == 0:
		icon = "speaker.slash.fill"
	case vol < 0.5:
		icon = "speaker.wave.1.fill"
	}
	if symbolButton(c, icon, 14, white, "静音").Clicked() {
		app.lib.Volume = ternary(vol > 0, 0.0, 1.0)
		app.lib.MarkDirty()
		if p := w.player; p != nil {
			p.SetVolume(app.lib.Volume)
		}
	}
	s := ui.SliderBase(c.Key("volume"), &app.lib.Volume, 0, 1).Width(84).Height(18).Label("音量")
	if s.Changed() {
		app.lib.MarkDirty()
		if p := w.player; p != nil {
			p.SetVolume(app.lib.Volume)
		}
	}
	at := float32(app.lib.Volume)
	s.Draw(func(pt *ui.Painter, r ui.Rect) {
		y := r.Y + r.H/2 - 1.5
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 3}, white.Alpha(0.25), 1.5)
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W * at, H: 3}, white, 1.5)
		pt.Fill(ui.Rect{X: r.X + r.W*at - 5, Y: r.Y + r.H/2 - 5, W: 10, H: 10}, white, 5)
	})
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
	s := ui.SliderBase(c.Key("seek"), &w.seek, 0, dur).Height(20).Label("进度").Disabled(pl == nil)
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
	ui.Column(c).Width(368).Shrink(0).Background(p.bg).BorderWidth(0, 0, 0, 1).BorderColor(p.hairline).Children(func() {
		ui.Box(c).Height(max(c.TitleBar().Height, 28) + 12).DragWindow()
		ui.Scroll(c.Key("side")).TrackScroll(&w.side).Grow(1).Padding(0, 20, 28).Gap(24).Children(func() {
			app.about(c, w)
			app.seasonRow(c, w)
			if w.detail != nil {
				app.episodes(c, w)
			} else if w.detailErr == "" {
				sh := app.shimmer(c)
				ui.Grid(c).Columns(6).Gap(8).Children(func() {
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
			ui.Text(c, w.show.Title).FontSize(17).Bold().MaxLines(3).TextColor(p.text)
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
		d := ui.Column(c.Key("desc")).Cursor(ui.CursorPointer).Label("简介")
		d.Children(func() {
			t := ui.Text(c, w.show.Desc).FontSize(12.5).LineHeight(1.55).TextColor(p.muted)
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
			ui.Text(c, "选集").FontSize(15).Bold().TextColor(p.text).Grow(1)
			ui.Text(c, fmt.Sprintf("第%d集 / 共%d集", w.index+1, len(eps))).FontSize(12).TextColor(p.muted)
		})
		ranges := (len(eps) + episodesPerRange - 1) / episodesPerRange
		if ranges > 1 {
			ui.ScrollHorizontal(c.Key("ranges")).Gap(6).Children(func() {
				for r := range ranges {
					from, to := r*episodesPerRange+1, min((r+1)*episodesPerRange, len(eps))
					if app.chip(c.Key(r), fmt.Sprintf("%d-%d", from, to), r == w.epRange).Shrink(0).Clicked() {
						w.epRange = r
					}
				}
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
				cell := ui.ButtonBase(c.Key(ep.ID)).Height(36).Radius(9).Center().FocusRing(false).Label(fmt.Sprintf("第%d集", i+1))
				rest := ternary(current, p.accent, p.surface)
				pressable(cell, rest, ternary(current, rest, p.surface.Mix(p.text, 0.08)), 0.92)
				cell.Children(func() {
					if current && playing {
						sfsymbols.Symbol(c, "waveform").Size(13).Color(white)
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
						sfsymbols.Symbol(c, "lock.fill").Size(7).Color(p.faint).Absolute().Top(4).Right(5)
					}
				})
				if cell.Clicked() && !current {
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
		ui.Text(c, "相关推荐").FontSize(15).Bold().TextColor(p.text).Margin(0, 0, 8)
		for i, s := range w.recs {
			if i >= 12 {
				break
			}
			row := ui.ButtonBase(c.Key("rec-" + s.ID)).Gap(12).Padding(6).Radius(10).AlignItems(ui.Start).FocusRing(false).Label(s.Title)
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
			ui.Text(c, "系列").FontSize(15).Bold().TextColor(p.text).Grow(1)
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
				if !app.chip(c.Key("season-"+s.ID), label, current).Shrink(0).Label(label).Clicked() || current {
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
