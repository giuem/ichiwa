package app

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"

	"hongguo/internal/catalog"
	"hongguo/internal/mac"
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

	index     int
	gen       int // counts episodes started, to drop stale results
	player    *mac.Player
	bitmap    *ui.Bitmap
	frameSize image.Point
	stop      chan struct{}
	loading   bool
	err       string
	ended     bool
	paused    bool // the user paused
	auto      bool // the next play call comes from autoplay
	skipped   int  // episodes autoplay skipped in a row
	retryAt   time.Time

	seek     float64
	dragging bool
	panel    bool
	descOpen bool
	epRange  int
	side     ui.ScrollState
}

// open shows the player page for a drama, at episode ep, or where the
// user left it when ep is -1.
func (app *App) open(s Show, ep int) {
	if app.watch != nil {
		app.watch.close()
	}
	w := &Watching{app: app, show: s, index: max(ep, 0), loading: true, panel: true}
	app.watch = w
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
	w.app.lib.Progress(w.show, w.index, w.player.Position(), w.player.Duration())
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
	w.bitmap = nil
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
		var p *mac.Player
		if err == nil {
			p, err = mac.Open(path)
		}
		app.win.Update(func() {
			if app.watch != w || w.gen != gen {
				if p != nil {
					p.Close()
				}
				return
			}
			w.loading = false
			if err != nil {
				w.failed(i, auto, err)
				return
			}
			w.skipped = 0
			w.player = p
			p.SetVolume(app.lib.Volume)
			if at > 1 {
				p.Seek(at - 1)
			}
			p.Play()
			if app.lib.Speed != 1 {
				p.SetRate(app.lib.Speed)
			}
			w.stop = make(chan struct{})
			go w.pump(gen, p, w.stop)
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

// pump copies the frames AVPlayer shows into the bitmap the view draws,
// and notices the end of the episode.
func (w *Watching) pump(gen int, p *mac.Player, stop chan struct{}) {
	var bufs [4]*image.RGBA
	n := 0
	tick := time.NewTicker(6 * time.Millisecond)
	defer tick.Stop()
	lastCheck := time.Now()
	endedSent := false
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		if img, ok := p.Frame(bufs[n%len(bufs)]); ok {
			bufs[n%len(bufs)] = img
			n++
			mygo.RunOnMain(func() {
				if w.gen != gen || w.app.watch != w {
					return
				}
				if w.bitmap == nil || w.frameSize != img.Rect.Size() {
					w.bitmap = ui.NewBitmap(img)
					w.frameSize = img.Rect.Size()
				} else {
					w.bitmap.Update(img)
				}
				w.app.win.Invalidate()
			})
		}
		if time.Since(lastCheck) > 250*time.Millisecond {
			lastCheck = time.Now()
			dur, pos := p.Duration(), p.Position()
			failed := p.Failed()
			if !endedSent && (failed || dur > 0 && pos >= dur-0.3) {
				endedSent = true
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
			}
		}
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

func (w *Watching) onEnded() {
	if w.player != nil {
		w.saveProgress()
	}
	if w.index+1 < len(w.detail.Episodes) {
		w.auto = true
		w.play(w.index+1, 0)
		return
	}
	w.ended = true
}

func (w *Watching) togglePlay() {
	p := w.player
	if p == nil {
		return
	}
	if w.ended {
		w.ended = false
		p.Seek(0)
	}
	if p.Playing() {
		p.Pause()
		w.paused = true
		w.saveProgress()
	} else {
		p.Play()
		if w.app.lib.Speed != 1 {
			p.SetRate(w.app.lib.Speed)
		}
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
	}
}

func (w *Watching) seekBy(d float64) {
	if p := w.player; p != nil {
		p.Seek(min(max(p.Position()+d, 0), p.Duration()))
		w.syncNowPlaying()
	}
}

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
			app.lib.Progress(w.show, w.index, w.player.Position(), w.player.Duration())
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
			if w.bitmap != nil {
				ui.Image(c, w.bitmap).Fill().Fit(ui.Contain)
			} else if bmp := app.posters.Get(w.show.Cover); bmp != nil {
				ui.Image(c, bmp).Fill().Fit(ui.Cover).Opacity(0.3).Blur(40)
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
				hasNext := w.detail != nil && w.index+1 < len(w.detail.Episodes)
				if symbolButton(c, "backward.end.fill", 15, white, "上一集 (↑)").Disabled(!hasPrev).Clicked() {
					w.step(-1)
				}
				pb := roundButton(c, 22, white, "播放/暂停 (空格)")
				pb.Children(func() { playPause(c, pb, playing, 22) })
				if pb.Clicked() {
					w.togglePlay()
				}
				if symbolButton(c, "forward.end.fill", 15, white, "下一集 (↓)").Disabled(!hasNext).Clicked() {
					w.step(1)
				}
				if pl := w.player; pl != nil {
					ui.Row(c).Gap(4).Margin(0, 0, 0, 8).Children(func() {
						ui.Text(c, fmtTime(w.seek)).FontSize(12.5).FontWeight(500).TextColor(white).NoWrap()
						ui.Text(c, "/ "+fmtTime(pl.Duration())).FontSize(12.5).TextColor(white.Alpha(0.55)).NoWrap()
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
		ui.Text(c, "全剧终").FontSize(24).Bold().TextColor(white)
		ui.Text(c, "已看完全部剧集").FontSize(13).TextColor(dim)
		app.primary(c, iconRefresh, "从头再看").Margin(8, 0, 0).OnClick(func() { w.play(0, 0) })
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
		if p := w.player; p != nil && p.Playing() {
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
		dur = max(pl.Duration(), 1)
		if !w.dragging {
			w.seek = pl.Position()
		}
	} else {
		w.seek = 0
	}
	s := ui.SliderBase(c.Key("seek"), &w.seek, 0, dur).Height(20).Label("进度").Disabled(pl == nil)
	if s.Changed() && pl != nil {
		pl.Seek(w.seek)
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
