package ui

import (
	"fmt"
	"strings"
	"time"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

// miniFixFor is how long the small window holds its size after full screen.
const miniFixFor = 1500 * time.Millisecond

// keyLinger is how long the controls show after a key acted on the video.
const keyLinger = 1500 * time.Millisecond

// chromeLinger is how long full screen keeps the title bar and the controls
// after the pointer last moved.
const chromeLinger = 3 * time.Second

// toggleMini asks for the small window, or for the window it was; a full
// screen window keeps its size.
func (app *App) toggleMini() {
	if app.Watch != nil && !app.win.IsFullScreen() {
		app.Watch.ToggleMini()
	}
}

// syncOnTop tells the window to stay on top or not: a full screen window
// has nothing to stay above.
func (app *App) syncOnTop(full bool) { app.SetAlwaysOnTop(app.Lib.OnTop && !full) }

// watchUI is what the view keeps of the player page.
type watchUI struct {
	seekBar ui.Handle
	side    ui.ScrollState
	// dm lays out the comments of dmFor, of which it took dmTaken.
	dm      *danmaku
	dmFor   *core.Danmaku
	dmTaken int
}

// uiOf is the view's state of the player page w.
func uiOf(w *Watching) *watchUI {
	if u, ok := w.UI.(*watchUI); ok {
		return u
	}
	u := &watchUI{}
	w.UI = u
	return u
}

// syncNarrow takes the width of a panel folded by pinning off the window,
// as far as the minimum size lets it, and gives that back when the panel
// opens, the window unpins or the page closes. It waits out full screen,
// which keeps its own size, and leaves a maximized window alone.
func (app *App) syncNarrow() {
	if app.MiniOn || app.mini { // that one has the size until it ends
		return
	}
	w := app.Watch
	want := w != nil && app.Lib.OnTop && w.Unpin
	if want == app.narrowed || app.win.IsFullScreen() || want && app.win.IsMaximized() {
		return
	}
	app.narrowed = want
	// After this build: resizing makes a frame of its own.
	app.win.Update(func() {
		width, height := app.win.Size()
		if want {
			app.narrowedBy = core.Narrowing(width)
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
		app.win.SetMinimumSize(core.MinWindowWidth, core.MinWindowHeight)
	}
	if !app.narrowed || app.win.IsFullScreen() {
		return
	}
	app.narrowed = false
	width, height := app.win.Size()
	app.win.SetSize(width+app.narrowedBy, height)
}

// syncMini resizes the window for the small window, or back from it. Like
// syncNarrow, it waits out full screen.
func (app *App) syncMini() {
	want := app.Watch != nil && app.MiniOn
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
	if want && app.Watch.Player != nil && app.Watch.Player.Ready() {
		vw, vh = app.Watch.Player.Size()
	}
	// After this build: resizing makes a frame of its own.
	app.win.Update(func() {
		if want {
			app.miniBackW, app.miniBackH = app.win.Size()
			app.win.SetMinimumSize(core.MiniMinWidth, core.MiniMinHeight)
			app.miniW, app.miniH = core.MiniSize(vw, vh)
			app.win.SetSize(app.miniW, app.miniH)
		} else {
			app.win.SetSize(app.miniBackW, app.miniBackH)
			app.win.SetMinimumSize(core.MinWindowWidth, core.MinWindowHeight)
		}
	})
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
	w := app.Watch
	// A click leaves the focus alone, or the button clicked would take Space
	// and the sliders the arrows from the shortcuts below. Tab still reaches them.
	c.Root().Background(app.pal.bg).KeepFocus()
	full := app.win.IsFullScreen()
	app.syncOnTop(full)

	// Keyboard.
	c.OnShortcut(0, ui.KeySpace, w.TogglePlay)
	c.OnShortcut(0, ui.KeyLeft, func() { w.SeekBy(-5) })
	c.OnShortcut(0, ui.KeyRight, func() {
		if w.RightAt.IsZero() {
			w.RightAt = time.Now()
		}
	})
	w.HoldRight(c.KeyDown(ui.KeyRight), time.Now())
	if !w.RightAt.IsZero() && !w.Boost {
		c.After(50 * time.Millisecond) // to notice the hold
	}
	c.OnShortcut(0, ui.KeyUp, func() { w.Step(-1) })
	c.OnShortcut(0, ui.KeyDown, func() { w.Step(1) })
	c.OnShortcut(0, ui.KeyF, app.win.ToggleFullScreen)
	width, _ := c.Size()
	if !core.DrawerFits(width, full) || w.Detail == nil {
		w.Drawer = false
	}
	c.OnShortcut(0, ui.KeyE, func() { w.ToggleEpisodes(width, full) })
	c.OnShortcut(0, ui.KeyA, w.ToggleAutoplay)
	if w.Has(source.CapDanmaku) {
		c.OnShortcut(0, ui.KeyD, w.ToggleDanmaku)
	}
	c.OnShortcut(0, ui.KeyV, app.toggleMini)
	c.OnShortcut(0, ui.KeyM, w.ToggleMute)
	c.OnShortcut(0, ui.KeyMinus, func() { w.NudgeVolume(-0.1) })
	c.OnShortcut(0, ui.KeyEqual, func() { w.NudgeVolume(0.1) })
	c.OnShortcut(0, ui.KeyBracketLeft, func() { w.StepSpeed(-1) })
	c.OnShortcut(0, ui.KeyBracketRight, func() { w.StepSpeed(1) })
	c.OnShortcut(0, ui.KeyT, func() {
		if !app.win.IsFullScreen() {
			w.ToggleOnTop()
		}
	})
	c.OnShortcut(0, ui.KeyEscape, func() {
		if w.Drawer {
			w.Drawer = false
			return
		}
		if app.win.IsFullScreen() {
			app.win.SetFullScreen(false)
			return
		}
		app.CloseWatch()
	})

	if news := w.News(); news != "" && news != w.Told {
		w.Told = news
		c.Announce(news)
	}

	playing := w.Player != nil && w.Player.Playing()
	if w.Player != nil {
		if playing {
			c.After(250 * time.Millisecond) // the clock and the progress bar
		}
		if !w.Dragging {
			w.SaveProgress()
		}
	}

	w.SyncDanmaku()

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		app.stage(c, w, playing, full)
		if w.Panel && !full && app.panelRoom(c) {
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
			sfIcon(c, icon.name).Size(size).Color(white).Absolute().Top(0).Left(0).Scale(s, s).Opacity(min(icon.shown, 1))
		}
	})
}

// panelRoom tells the window has room for the side panel now.
func (app *App) panelRoom(c *ui.Context) bool {
	width, _ := c.Size()
	return core.PanelFits(width)
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
	px, py := pointerAt(stage)
	still := w.PointerStill(px, py, playing)
	// The keyboard's focus on a control keeps the controls in view.
	shown := chromeShown(playing, hover, full, still) || time.Since(w.KeyAt) < keyLinger || w.Drawer || w.DmOpen || w.SpeedOpen || w.VolOpen || stage.FocusWithin()
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
		toggle, toFull := w.PictureClicks(video.Clicked(), video.DoubleClicked(), video.Pressed(), time.Now())
		if toggle {
			w.TogglePlay()
		}
		if toFull {
			app.win.ToggleFullScreen()
		}
		if !w.ClickAt.IsZero() {
			c.After(core.ClickDelay)
		}
		video.Children(func() {
			// Built before its first picture too: only then does it start.
			if w.Player != nil {
				ui.Video(c, rawPlayer(w.Player)).Fill().Fit(ui.Contain)
			}
			app.danmakuLayer(c, w)
			if w.Player == nil || !w.Player.Ready() {
				if bmp := app.posters.Get(w.Show.Image); bmp != nil {
					ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Opacity(0.3).Blur(40)
				}
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(14).PassThrough().Children(func() {
				app.stageMessage(c, w)
			})
			if w.Boost {
				ui.Box(c).Absolute().Top(64).Left(0).Right(0).Center().PassThrough().Children(func() {
					ui.Row(c).Gap(5).Padding(6, 12).Radius(999).Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
						sfIcon(c, "forward.fill").Size(12).Color(white)
						ui.Text(c, fmt.Sprintf("%gx 快进中", float64(core.BoostRate))).FontSize(12.5).FontWeight(600).TextColor(white)
					})
				})
			}
		})

		// Title bar.
		ui.Row(c).Padding(max(bar.Height, 28)/2-2, bar.Right+14, 34, bar.Left+14).Gap(8).DragWindow().
			Gradient(ui.RGBA(0, 0, 0, 0.7), ui.Transparent, 180).Opacity(max(show, boolf(!playing))).
			Children(func() {
				if symbolButton(c, "chevron.left", 15, white, "返回 (Esc)").KeyShortcuts(0, ui.KeyEscape).Clicked() {
					app.CloseWatch()
				}
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, w.Show.Title).FontSize(15).Bold().TextColor(white).SingleLine().Role(ui.RoleHeading).Level(1)
					if w.Detail != nil {
						ui.Text(c, fmt.Sprintf("第%d集 · 共%d集", w.Index+1, len(w.Detail.Episodes))).FontSize(11.5).TextColor(white.Alpha(0.65))
					}
				})
				if !full && app.panelRoom(c) && symbolButton(c, "sidebar.right", 15, white, "选集面板").Expanded(w.Panel).Clicked() {
					w.Panel, w.Unpin = !w.Panel, false
				}
			})

		// Painted above the picture, which is under the flow: it must let the
		// pointer through or the picture gets no clicks.
		ui.Spacer(c).PassThrough()

		// Controls.
		// What does not fit a narrow stage goes first, for keys and the menu.
		width, _ := c.Size()
		stageWidth := width
		if w.Panel && !full && app.panelRoom(c) {
			stageWidth -= core.PanelWidth
		}
		tiny := stageWidth < 460
		ui.Column(c).Padding(40, 18, 12).Gap(4).Gradient(ui.Transparent, ui.RGBA(0, 0, 0, 0.78), 180).Opacity(show).Children(func() {
			app.progress(c, w)
			ui.Row(c).Gap(2).Children(func() {
				hasPrev := w.Detail != nil && w.Index > 0
				next, hasNextSeason := w.NextSeason()
				hasNextSeason = hasNextSeason && next.IsAvailable() && w.LastEpisode()
				hasNext := w.Detail != nil && w.Index+1 < len(w.Detail.Episodes) || hasNextSeason
				if !tiny && symbolButton(c, "backward.end.fill", 15, white, "上一集 (↑)").KeyShortcuts(0, ui.KeyUp).Disabled(!hasPrev).Clicked() {
					w.Step(-1)
				}
				pb := roundButton(c, 22, white, ternary(playing, "暂停 (空格)", "播放 (空格)")).KeyShortcuts(0, ui.KeySpace)
				pb.Children(func() { playPause(c, pb, playing, 22) })
				if pb.Clicked() {
					w.TogglePlay()
				}
				if !tiny && symbolButton(c, "forward.end.fill", 15, white, ternary(hasNextSeason, "下一季 (↓)", "下一集 (↓)")).KeyShortcuts(0, ui.KeyDown).Disabled(!hasNext).Clicked() {
					w.Step(1)
				}
				if pl := w.Player; pl != nil {
					ui.Row(c).Gap(4).Margin(0, 0, 0, 8).Children(func() {
						ui.Text(c, fmtTime(w.Seek)).FontSize(12.5).FontWeight(500).TextColor(white).NoWrap()
						ui.Text(c, "/ "+fmtTime(pl.Duration().Seconds())).FontSize(12.5).TextColor(white.Alpha(0.55)).NoWrap()
					})
				}
				ui.Spacer(c)
				app.speedButton(c, w)
				if !tiny {
					app.volume(c, w)
				} else {
					w.VolOpen = false
				}
				if w.Has(source.CapDanmaku) {
					if symbolButton(c, "text.bubble.fill", 14, ternary(app.Lib.Danmaku, app.pal.accent, white), "弹幕 (D)").KeyShortcuts(0, ui.KeyD).Role(ui.RoleToggleButton).Checked(app.Lib.Danmaku).Clicked() {
						w.ToggleDanmaku()
					}
					if app.Lib.Danmaku && !tiny {
						sb := symbolButton(c.Key("dm-settings"), "slider.horizontal.3", 14, ternary(w.DmOpen, app.pal.accent, white), "弹幕设置").Expanded(w.DmOpen)
						if sb.Clicked() {
							w.DmOpen = !w.DmOpen
						}
						app.danmakuSettings(c, w, sb)
					} else {
						w.DmOpen = false
					}
				} else {
					w.DmOpen = false
				}
				if core.DrawerFits(width, full) && w.Detail != nil && symbolButton(c, "list.number", 14, ternary(w.Drawer, app.pal.accent, white), "选集 (E)").KeyShortcuts(0, ui.KeyE).Clicked() {
					w.ToggleEpisodes(width, full)
				}
				symbolButton(c.Key("more"), "ellipsis", 14, white, "更多").Menu(app.playerMenu(w, full))
				fs := ternary(full, "arrow.down.right.and.arrow.up.left", "arrow.up.left.and.arrow.down.right")
				if !full && !tiny && symbolButton(c, "pin.fill", 14, ternary(app.Lib.OnTop, app.pal.accent, white), "窗口置顶 (T)").KeyShortcuts(0, ui.KeyT).Role(ui.RoleToggleButton).Checked(app.Lib.OnTop).Clicked() {
					w.ToggleOnTop()
				}
				if symbolButton(c, fs, 14, white, ternary(full, "退出全屏 (F)", "全屏 (F)")).KeyShortcuts(0, ui.KeyF).Clicked() {
					app.win.ToggleFullScreen()
				}
			})
		})

		if w.Drawer {
			app.drawer(c, w, width)
		}
	})
}

// drawer shows the episodes over the picture. A click beside it closes it,
// and does not reach the picture.
func (app *App) drawer(c *ui.Context, w *Watching, width float32) {
	p := app.pal
	if ui.Box(c.Key("drawer-scrim")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Label("关闭选集").Clicked() {
		w.Drawer = false
	}
	ui.Column(c.Key("drawer")).Absolute().Top(0).Right(0).Bottom(0).Width(min(core.PanelWidth, width-24)).
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
		if m.Item("自动连播").Checked(app.Lib.Autoplay).Trailing("A").Chosen() {
			w.ToggleAutoplay()
		}
		if w.Has(source.CapDanmaku) {
			if m.Item("弹幕").Checked(app.Lib.Danmaku).Trailing("D").Chosen() {
				w.ToggleDanmaku()
			}
		}
		sleep := m.Submenu("定时停止", func(m *ui.Menu) {
			for _, o := range core.SleepOptions {
				if m.Item(o.Label).Radio(app.SleepMins == o.Mins).Chosen() {
					app.SetSleep(o.Mins)
					app.Toast = ternary(o.Mins == 0, "已关闭定时停止", "定时停止："+o.Label)
				}
			}
		})
		if app.SleepMins == core.SleepEpisode {
			sleep.Trailing("本集")
		} else if !app.SleepAt.IsZero() {
			sleep.Trailing(fmt.Sprintf("%d 分钟", int(time.Until(app.SleepAt).Minutes())+1))
		}
		if full {
			return
		}
		m.Separator()
		if m.Item("小窗模式").Checked(app.MiniOn).Trailing("V").Chosen() {
			app.toggleMini()
		}
		if m.Item("窗口置顶").Checked(app.Lib.OnTop).Trailing("T").Chosen() {
			w.ToggleOnTop()
		}
	}
}

// stageMessage is what shows over the picture: loading, errors, the end.
func (app *App) stageMessage(c *ui.Context, w *Watching) {
	dim := white.Alpha(0.7)
	switch {
	case w.DetailErr != "":
		sfIcon(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.DetailErr).FontSize(15).TextColor(white).Role(ui.RoleAlert)
		app.secondary(c, nil, "返回").OnClick(app.CloseWatch)
	case w.Err != "":
		sfIcon(c, "exclamationmark.triangle").Size(30).Color(dim)
		ui.Text(c, w.Err).FontSize(15).TextColor(white).Role(ui.RoleAlert)
		ui.Row(c).Gap(10).Margin(6, 0, 0).Children(func() {
			label := "重试"
			wait := w.RetryWait()
			if wait > 0 {
				label = fmt.Sprintf("重试 (%ds)", wait)
			}
			app.secondary(c, iconRefresh, label).Disabled(wait > 0).OnClick(func() {
				if w.RetryWait() == 0 {
					w.Play(w.Index, w.ResumeAt())
				}
			})
			if w.Detail != nil && w.Index+1 < len(w.Detail.Episodes) {
				app.primary(c, nil, "下一集").OnClick(func() { w.Step(1) })
			} else if next, ok := w.NextSeason(); ok && next.IsAvailable() {
				app.primary(c, nil, "下一季").OnClick(func() { w.Step(1) })
			}
		})
	case w.Loading:
		ui.Spinner(c).Size(30, 30)
		label := "正在加载…"
		if w.Detail != nil {
			label = fmt.Sprintf("正在加载第%d集…", w.Index+1)
		}
		ui.Text(c, label).FontSize(13).TextColor(dim)
	case w.Halt:
		ui.Text(c, fmt.Sprintf("第%d集播完", w.Index+1)).FontSize(24).Bold().TextColor(white)
		ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
			app.secondary(c, iconRefresh, "重播").OnClick(func() { w.Play(w.Index, 0) })
			app.primary(c, nil, ternary(w.LastEpisode(), "下一季", "下一集")).OnClick(func() { w.Step(1) })
		})
	case w.Ended:
		next, more := w.NextSeason()
		if !more {
			ui.Text(c, "全剧终").FontSize(24).Bold().TextColor(white)
			ui.Text(c, "已看完全部剧集").FontSize(13).TextColor(dim)
			app.primary(c, iconRefresh, "从头再看").Margin(8, 0, 0).OnClick(func() { w.Play(0, 0) })
			return
		}
		cur := w.Seasons[core.SeasonOf(w.Seasons, w.Show.ID)]
		ui.Text(c, cur.Title+"完").FontSize(24).Bold().TextColor(white)
		ui.Text(c, seasonLine(next)).FontSize(13).TextColor(dim)
		nextRef := source.Ref{Source: w.Ref().Source, ID: next.ID}
		ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
			app.secondary(c, iconRefresh, "从头再看").OnClick(func() { w.Play(0, 0) })
			switch {
			case next.IsAvailable():
				app.primary(c, nil, "看"+next.Title).OnClick(func() { w.SwitchSeason(next, -1) })
			case app.Lib.Following(nextRef):
				app.secondary(c, nil, "已追"+next.Title).OnClick(func() { app.ToggleFollow(nextRef.Source, core.SeasonCard(next)) })
			default:
				app.primary(c, iconHeart, "追"+next.Title).OnClick(func() { app.ToggleFollow(nextRef.Source, core.SeasonCard(next)) })
			}
		})
	case w.Paused && w.Player != nil:
		ui.Box(c).Size(76, 76).Radius(999).Background(ui.RGBA(0, 0, 0, 0.45)).Center().
			Border(1, white.Alpha(0.15)).Children(func() {
			sfIcon(c, "play.fill").Size(30).Color(white).Margin(0, 0, 0, 4)
		})
	}
}

func (app *App) speedButton(c *ui.Context, w *Watching) {
	speed := app.Lib.Speed
	b := ui.ButtonBase(c.Key("speed")).Height(30).MinWidth(30).PaddingX(8).Radius(999).Center().Label("播放速度").
		Value(fmt.Sprintf("%g倍", speed)).Expanded(w.SpeedOpen)
	tooltip(c, b, "播放速度 ([ / ])")
	pressable(b, ui.Transparent, white.Alpha(0.14), 0.92)
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%gx", speed)).FontSize(12.5).Bold().TextColor(ternary(w.SpeedOpen, app.pal.accent, white))
	})
	if b.Clicked() {
		w.SpeedOpen = !w.SpeedOpen
	}
	app.speedPicker(c, w, b)
}

// speedPicker is the list of speeds that opens from anchor, the fastest on
// top; choosing one closes it.
func (app *App) speedPicker(c *ui.Context, w *Watching, anchor ui.Element) {
	ui.PopoverBase(c, anchor, &w.SpeedOpen, func(panel ui.Element) {
		popPanel(panel, anchor).Width(96).Padding(6).Gap(2).Radius(12).Role(ui.RoleListBox).Label("播放速度")
		panel.Children(func() {
			for i := len(core.SpeedSteps) - 1; i >= 0; i-- {
				s := core.SpeedSteps[i]
				on := s == app.Lib.Speed
				o := ui.ButtonBase(c.Key(i)).Height(30).Radius(8).Center().Role(ui.RoleOption).
					Label(fmt.Sprintf("%g倍", s)).Checked(on)
				pressable(o, ternary(on, white.Alpha(0.1), ui.Transparent), white.Alpha(0.14), 0.96)
				o.Children(func() {
					ui.Text(c, fmt.Sprintf("%gx", s)).FontSize(13).FontWeight(ternary(on, 700, 500)).
						TextColor(ternary(on, app.pal.accent, white))
				})
				if o.Clicked() {
					w.SetSpeed(s)
					w.SpeedOpen = false
				}
			}
		})
	})
}

// volume is the speaker button, which opens a panel of the volume: how
// loud it is, a slider and a button to mute.
func (app *App) volume(c *ui.Context, w *Watching) {
	vol := app.Lib.Volume
	b := symbolButton(c.Key("volume"), volumeSymbol(vol), 14, ternary(w.VolOpen, app.pal.accent, white), "音量 (M 静音)").
		Value(fmt.Sprintf("%d%%", int(vol*100+0.5))).Expanded(w.VolOpen)
	if b.Clicked() {
		w.VolOpen = !w.VolOpen
	}
	ui.PopoverBase(c, b, &w.VolOpen, func(panel ui.Element) {
		popPanel(panel, b).Width(52).Padding(12, 0, 8).Gap(8).Radius(12).AlignItems(ui.Center).Label("音量")
		panel.Children(func() {
			ui.Text(c, fmt.Sprintf("%d", int(app.Lib.Volume*100+0.5))).FontSize(12.5).FontWeight(600).TextColor(white)
			s := ui.SliderBase(c.Key("slider"), &app.Lib.Volume, 0, 1).Vertical().Width(24).Height(110).PaddingY(7).
				Label("音量").Value(fmt.Sprintf("%d%%", int(app.Lib.Volume*100+0.5)))
			if s.Changed() {
				app.Lib.MarkDirty()
				if p := w.Player; p != nil {
					p.SetVolume(app.Lib.Volume)
				}
			}
			hot := s.AnimateTo("hot", boolf(s.Hovered() || s.Dragging()), ui.Smooth)
			at := float32(app.Lib.Volume)
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
			if symbolButton(c.Key("mute"), volumeSymbol(app.Lib.Volume), 13, white.Alpha(0.85), ternary(app.Lib.Volume > 0, "静音", "取消静音")).Clicked() {
				w.ToggleMute()
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
	pl := w.Player
	dur := 1.0
	if pl != nil {
		dur = max(pl.Duration().Seconds(), 1)
		if !w.Dragging {
			w.Seek = pl.Position().Seconds()
		}
	} else {
		w.Seek = 0
	}
	box := ui.Box(c)
	var s ui.Element
	box.Children(func() {
		s = ui.SliderBase(c.Key("seek"), &w.Seek, 0, dur).Height(20).Label("进度").Disabled(pl == nil).Bind(&uiOf(w).seekBar).
			Value(spokenTime(w.Seek) + "，共" + spokenTime(dur))
		// The time under the pointer, over the bar.
		x, _, over := s.PointerPosition()
		if width := uiOf(w).seekBar.Bounds(c).W; pl != nil && width > 0 && (over || s.Dragging()) {
			const tip = 56
			x = min(max(x, 0), width)
			ui.Box(c).Width(tip).Padding(3, 0).Radius(5).Center().Background(ui.RGBA(0, 0, 0, 0.72)).
				Absolute().Bottom(24).Left(min(max(x-tip/2, 0), width-tip)).PassThrough().Children(func() {
				ui.Text(c, fmtTime(float64(x/width)*dur)).FontSize(11.5).FontWeight(600).TextColor(white).NoWrap()
			})
		}
	})
	if s.Changed() && pl != nil {
		pl.Seek(core.Seconds(w.Seek))
	}
	if w.Dragging && !s.Dragging() {
		w.SyncNowPlaying()
	}
	w.Dragging = s.Dragging()
	hot := s.AnimateTo("hot", boolf(s.Hovered() || w.Dragging), ui.Smooth)
	at := float32(w.Seek / dur)
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
	ui.Column(c).Width(core.PanelWidth).Shrink(0).Background(p.bg).BorderWidth(0, 0, 0, 1).BorderColor(p.hairline).Children(func() {
		ui.Box(c).Height(max(c.TitleBar().Height, 28) + 12).DragWindow()
		ui.Scroll(c.Key("side")).TrackScroll(&uiOf(w).side).Grow(1).Padding(0, 20, 28).Gap(24).Children(func() {
			app.about(c, w)
			app.seasonRow(c, w)
			if w.Detail != nil {
				app.episodes(c, w)
			} else if w.DetailErr == "" {
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
	ref := w.Ref()
	ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
		app.poster(c, w.Show.Image, w.Show.Title, 10).Width(96).Shrink(0)
		ui.Column(c).Grow(1).Gap(7).Children(func() {
			ui.Text(c, w.Show.Title).FontSize(17).Bold().MaxLines(3).TextColor(p.text).Role(ui.RoleHeading).Level(2)
			var meta []string
			if w.Detail != nil {
				for _, m := range w.Detail.Meta {
					if m.Label != "" {
						meta = append(meta, m.Label+" "+m.Value)
					} else if m.Value != "" {
						meta = append(meta, m.Value)
					}
				}
				if len(meta) == 0 && len(w.Detail.Episodes) > 0 {
					meta = append(meta, fmt.Sprintf("全%d集", len(w.Detail.Episodes)))
				}
			}
			if len(meta) > 0 {
				ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(p.muted)
			}
			if w.Detail != nil {
				ui.Row(c).Gap(5).Wrap().Children(func() {
					shown := 0
					for _, tag := range w.Detail.Tags {
						if shown >= 4 || strings.HasPrefix(tag, "全") && strings.HasSuffix(tag, "集") {
							continue
						}
						shown++
						ui.Text(c, tag).FontSize(11).TextColor(p.muted).Padding(2, 8).Radius(5).Background(p.surface)
					}
				})
			}
			following := app.Lib.Following(ref)
			var b ui.Element
			if following {
				b = app.secondary(c, nil, "已追剧")
			} else {
				b = app.primary(c, iconHeart, "追剧")
			}
			b.Margin(4, 0, 0).AlignSelf(ui.Start)
			if b.Clicked() {
				app.ToggleFollow(ref.Source, w.Show)
			}
		})
	})
	desc := w.Show.Subtitle
	if w.Detail != nil && w.Detail.Description != "" {
		desc = w.Detail.Description
	}
	if desc != "" {
		// A button reads no text inside it: its Description reads the synopsis.
		d := ui.ButtonBase(c.Key("desc")).AlignItems(ui.Start).Justify(ui.Start).Shrink(1).Cursor(ui.CursorPointer).
			Label("简介").Description(desc).Expanded(w.DescOpen)
		d.Children(func() {
			t := ui.Text(c, desc).FontSize(12.5).LineHeight(1.55).TextColor(p.muted).Grow(1).Shrink(1)
			if !w.DescOpen {
				t.MaxLines(3)
			}
		})
		if d.Clicked() {
			w.DescOpen = !w.DescOpen
		}
	}
}

func (app *App) episodes(c *ui.Context, w *Watching) {
	p := app.pal
	ref := w.Ref()
	eps := w.Detail.Episodes
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "选集").FontSize(15).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			ui.Text(c, fmt.Sprintf("第%d集 / 共%d集", w.Index+1, len(eps))).FontSize(12).TextColor(p.muted)
		})
		ranges := (len(eps) + core.EpisodesPerRange - 1) / core.EpisodesPerRange
		if ranges > 1 {
			ui.ScrollHorizontal(c.Key("ranges")).Children(func() {
				choices(c, "集数范围").Gap(6).Children(func() {
					for r := range ranges {
						from, to := r*core.EpisodesPerRange+1, min((r+1)*core.EpisodesPerRange, len(eps))
						chip := app.chip(c.Key(r), fmt.Sprintf("%d-%d", from, to), r == w.EpRange).Shrink(0)
						if radio(chip, r == w.EpRange).Label(fmt.Sprintf("第%d到%d集", from, to)).Clicked() {
							w.EpRange = r
						}
					}
				})
			})
		}
		playing := w.Player != nil && w.Player.Playing()
		from := w.EpRange * core.EpisodesPerRange
		ui.Grid(c.Key(w.EpRange)).Columns(6).Gap(8).Children(func() {
			for i := from; i < min(from+core.EpisodesPerRange, len(eps)); i++ {
				ep := eps[i]
				current := i == w.Index
				locked := ep.Lock != source.LockNone
				done := app.Lib.Watched(ref, ep.ID)
				part := 0.0
				if e := app.Lib.Episode(ref, ep.ID); e != nil && !done && !current {
					part = e.Fraction()
				}
				note := ""
				switch {
				case locked:
					note = core.LockMessage(ep.Lock)
				case done:
					note = "已看完"
				case part > 0:
					note = fmt.Sprintf("看到%d%%", int(part*100))
				}
				cell := ui.ButtonBase(c.Key(ep.ID)).Height(36).Radius(9).Center().Clip().Label(fmt.Sprintf("第%d集", i+1)).Description(note)
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
					case locked:
						n.TextColor(p.faint)
					case done:
						n.TextColor(p.muted)
					default:
						n.TextColor(p.text)
					}
					switch {
					case locked:
						sfIcon(c, "lock.fill").Size(7).Color(p.faint).Absolute().Top(4).Right(5)
					case done && !current:
						sfIcon(c, "checkmark").Size(7).Color(p.faint).Absolute().Top(4).Right(5)
					case part > 0.02:
						// How far it got, along the cell's bottom.
						ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(2.5).Draw(func(pt *ui.Painter, r ui.Rect) {
							pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(part), H: r.H}, p.accent, 0)
						})
					}
				})
				if cell.Clicked() && !current {
					w.Drawer = false
					w.Play(i, app.Lib.EpisodeStart(ref, ep.ID))
				}
			}
		})
	})
}

func (app *App) related(c *ui.Context, w *Watching) {
	p := app.pal
	if w.Detail != nil && len(w.Detail.People) > 0 {
		ui.Column(c).Gap(10).Children(func() {
			ui.Text(c, "演职人员").FontSize(15).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(2)
			ui.ScrollHorizontal(c.Key("people")).Gap(12).Children(func() {
				for _, a := range w.Detail.People {
					b := ui.ButtonBase(c.Key("person-" + a.ID)).Width(64).Label(a.Title).Description(a.Subtitle)
					b.Children(func() {
						// A button lays its children in a row: the
						// avatar goes over its name and role.
						ui.Column(c).Width(64).Gap(6).AlignItems(ui.Center).Children(func() {
							app.avatar(c, a.Image, a.Title, 52)
							ui.Text(c, a.Title).FontSize(11.5).TextColor(ternary(b.Hovered(), p.accent, p.text)).SingleLine()
							if a.Subtitle != "" {
								ui.Text(c, a.Subtitle).FontSize(10.5).TextColor(p.muted).SingleLine()
							}
						})
					})
					if b.Clicked() {
						app.openFrom(w.SourceID, a)
					}
				}
			})
		})
	}
	for si, sec := range w.Related {
		if len(sec.Items) == 0 {
			continue
		}
		title := ternary(sec.Title != "", sec.Title, "相关推荐")
		ui.Column(c.Key(si)).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(15).Bold().TextColor(p.text).Margin(0, 0, 8).Role(ui.RoleHeading).Level(2)
			for i, s := range sec.Items {
				if i >= 12 {
					break
				}
				badge := cardBadge(s)
				row := ui.ButtonBase(c.Key("rec-" + s.ID)).Gap(12).Padding(6).Radius(10).AlignItems(ui.Start).Label(s.Title).
					Description(joinNonEmpty(badge, s.Subtitle))
				pressable(row, ui.Transparent, p.hover, 0.98)
				row.Children(func() {
					if s.Shape == source.ShapeCircle {
						app.avatar(c, s.Image, s.Title, 56)
					} else {
						app.poster(c, s.Image, s.Title, 7).Width(56).Shrink(0)
					}
					ui.Column(c).Grow(1).Gap(4).Padding(2, 0).Children(func() {
						ui.Text(c, s.Title).FontSize(13).FontWeight(600).MaxLines(2).TextColor(p.text)
						if badge != "" {
							ui.Text(c, badge).FontSize(11.5).TextColor(p.muted)
						}
						if s.Subtitle != "" {
							ui.Text(c, s.Subtitle).FontSize(11.5).TextColor(p.faint).MaxLines(2)
						}
					})
				})
				if row.Clicked() {
					app.openFrom(w.SourceID, s)
				}
			}
		})
	}
}

// seasonLine says where a season stands: when it comes out, that it is
// new, how many wait for it.
func seasonLine(s Season) string {
	parts := []string{s.Title}
	switch {
	case !s.IsAvailable():
		parts = append(parts, ternary(s.Note != "", s.Note, "即将上线"))
	case s.Note != "":
		parts = append(parts, s.Note)
	}
	return strings.Join(parts, " · ")
}

// seasonRow switches between the seasons of a drama that has several.
func (app *App) seasonRow(c *ui.Context, w *Watching) {
	p := app.pal
	if core.SeasonOf(w.Seasons, w.Show.ID) < 0 {
		return
	}
	latest := w.Seasons[len(w.Seasons)-1]
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "系列").FontSize(15).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			ui.Text(c, fmt.Sprintf("共%d季", len(w.Seasons))).FontSize(12).TextColor(p.muted)
		})
		ui.ScrollHorizontal(c.Key("seasons")).Gap(6).Children(func() {
			for _, s := range w.Seasons {
				label := s.Title
				switch {
				case !s.IsAvailable():
					label += " · 预告"
				case s.Note != "":
					label += " · 新"
				}
				current := s.ID == w.Show.ID
				chip := app.chip(c.Key("season-"+s.ID), label, current).Shrink(0).Label(label)
				if current {
					chip.Current(ui.CurrentItem)
				}
				if !chip.Clicked() || current {
					continue
				}
				if s.IsAvailable() {
					w.SwitchSeason(s, -1)
				} else {
					app.Toast = seasonLine(s)
				}
			}
		})
		if !latest.IsAvailable() || latest.Note != "" {
			ui.Text(c, seasonLine(latest)).FontSize(12).TextColor(p.muted)
		}
	})
}
