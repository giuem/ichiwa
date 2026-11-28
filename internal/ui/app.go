// Package ui draws the app in MyGo's native UI: the window, its pages,
// the player, the menu bar and the media keys. It reads the state of
// internal/app and takes the user's actions to it.
package ui

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"

	core "github.com/giuem/ichiwa/internal/app"
	"github.com/giuem/ichiwa/internal/source"
)

// The models of the protocol and of the app, as the views name them.
type (
	Card       = source.Card
	Media      = source.Media
	Season     = source.Season
	Section    = source.Section
	FilterRow  = source.FilterRow
	Record     = core.Record
	Feed       = core.Feed
	Watching   = core.Watching
	Collection = core.Collection
)

// App draws the app: the state is internal/app's, and it keeps only what
// is the window's own.
type App struct {
	*core.App
	win     *mygo.Window
	pal     palette
	posters *Posters

	// narrowed tells the window gave up the width of the folded panel.
	narrowed bool
	// quitting tells the window to really close, not just hide.
	quitting bool
	// hideOnLeave hides the window once it has left full screen.
	hideOnLeave bool
	// narrowedBy is the width it gave up.
	narrowedBy int
	// mini tells the window is the small window, and miniBack is the size
	// it goes back to.
	mini                 bool
	miniBackW, miniBackH int
	miniW, miniH         int
	miniFull             bool // the small window is in full screen
	miniFixUntil         time.Time
	// onTop is what the window was last told about staying on top.
	onTop bool

	// Told is the page title screen readers were last told of.
	Told string
	// historyFilter is the history page's filter, in historyFilters.
	historyFilter int
	shelf         ui.ScrollState
	searchField   ui.Handle
	// menuFor is the source the menu bar was made for.
	menuFor source.Source

	// upd is a newer version found, which the sidebar marks, and its
	// dialog.
	upd updateState
}

// Post runs fn on the UI thread and redraws.
func (app *App) Post(fn func()) {
	if app.win != nil {
		app.win.Update(fn)
	}
}

// Sync runs fn on the UI thread and waits.
func (app *App) Sync(fn func()) { mygo.RunOnMain(fn) }

// Redraw redraws the window.
func (app *App) Redraw() {
	if app.win != nil {
		app.win.Invalidate()
	}
}

// videoPlayer is MyGo's player as the app's.
type videoPlayer struct{ *ui.VideoPlayer }

func (p videoPlayer) SetRate(r float64)   { p.VideoPlayer.SetRate(r) }
func (p videoPlayer) SetVolume(v float64) { p.VideoPlayer.SetVolume(v) }

// rawPlayer is the MyGo player under p, nil when there is none.
func rawPlayer(p core.Player) *ui.VideoPlayer {
	if v, ok := p.(videoPlayer); ok {
		return v.VideoPlayer
	}
	return nil
}

// OpenVideo opens a player for url, or the file path.
func (app *App) OpenVideo(url, path string) (core.Player, error) {
	var p *ui.VideoPlayer
	var err error
	if url != "" {
		p, err = video.OpenURL(url)
	} else {
		p, err = video.Open(path)
	}
	if err != nil {
		return nil, err
	}
	return videoPlayer{p}, nil
}

// KeepAwake keeps the display on until release.
func (app *App) KeepAwake(reason string) func() { return mygo.Power.KeepAwake(reason, true) }

// SetAlwaysOnTop keeps the window above others, or not.
func (app *App) SetAlwaysOnTop(on bool) {
	if app.win != nil && on != app.onTop {
		app.onTop = on
		app.win.SetAlwaysOnTop(on)
	}
}

const sidebarWidth = 220

//go:embed assets/logo.png
var logoPNG []byte

var logo, _ = ui.DecodeBitmap(logoPNG)

func (app *App) view(c *ui.Context) {
	app.syncMini()
	app.syncNarrow()
	if app.win != nil && app.Src != app.menuFor { // the menu bar's search follows the source
		app.menuFor = app.Src
		app.setMenu()
	}
	if app.Watch == nil {
		app.SetAlwaysOnTop(false) // only the player page stays on top
	}
	if c.Theme().Dark || app.Watch != nil {
		app.pal = darkPalette()
	} else {
		app.pal = lightPalette()
	}
	if c.Preferences().HighContrast {
		app.pal = app.pal.contrasted()
	}
	applyPalette(c, app.pal)
	if app.Toast != "" {
		c.Toast(app.Toast)
		app.Toast = ""
	}

	app.updateDialog(c)
	if app.Watch != nil {
		app.watchView(c)
		return
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	if app.HasCap(source.CapSearch) {
		c.OnShortcut(ui.Cmd, ui.KeyF, func() { app.Go(core.PageSearch); app.Search.Focus = true })
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0).Role(ui.RoleNavigation).Label("主导航")
		if !c.Vibrancy() {
			side.Background(app.pal.sidebar)
		}
		side.Children(func() { app.sidebar(c) })
		ui.Column(c.Key("main")).Grow(1).Background(app.pal.bg).Role(ui.RoleRegion).Label("内容").Children(func() {
			app.content(c)
		})
	})
	app.Lib.Flush()
}

func navIcon(ic source.Icon) *ui.SVG {
	switch ic {
	case source.IconHome:
		return iconHome
	case source.IconFilm:
		return iconFilm
	case source.IconSparkles:
		return iconSparkles
	case source.IconTrophy:
		return iconTrophy
	case source.IconFilter:
		return iconSliders
	case source.IconFire:
		return iconFlame
	default:
		return iconHome
	}
}

func (app *App) sidebar(c *ui.Context) {
	p := app.pal
	top := max(c.TitleBar().Height, 28) + 10
	ui.Column(c).Fill().Padding(top, 12, 14).Gap(2).DragWindow().Children(func() {
		app.sourceSwitch(c)
		if app.Src != nil {
			app.navSection(c, "发现")
		}
		for _, item := range app.Nav {
			app.navItem(c, navIcon(item.Icon), item.Title, item.ID, 0)
		}
		if app.HasCap(source.CapSearch) {
			app.navItem(c, iconSearch, "搜索", core.PageSearch, 0)
		}
		app.navSection(c, "我的")
		app.navItem(c, iconClock, "继续观看", core.PageHistory, len(app.Lib.History))
		app.navItem(c, iconHeart, "追剧", core.PageFollows, len(app.Lib.Follows))
		if app.Dev.Plugin != nil {
			b := ui.ButtonBase(c.Key("plugin-log")).Height(34).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label("插件日志")
			if app.Dev.LogOpen {
				b.Current(ui.CurrentPage).Background(p.press)
			}
			b.Children(func() {
				ui.Icon(c, iconScroll).Size(17, 17).TextColor(p.muted)
				ui.Text(c, "插件日志").FontSize(13.5).FontWeight(500).TextColor(p.text)
			})
			if b.Clicked() {
				app.Dev.LogOpen = true
			}
		}
		ui.Spacer(c)
		ui.Row(c).Padding(0, 8, 6).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Image(c, logo).Size(36, 36).Margin(-3, -3, -3, -3)
			ui.Text(c, "一集").FontSize(15).Bold().TextColor(p.text)
		})
		if v := mygo.App.Version(); v != "" {
			ui.Row(c).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "v"+v).FontSize(10.5).TextColor(p.faint)
				if up := app.upd.up; up != nil {
					b := ui.ButtonBase(c.Key("new-version")).Height(18).PaddingX(7).Radius(999).Center().FocusRing(false).
						Label("新版本 " + up.Version).Description("点击查看并安装")
					pressable(b, p.accent.Alpha(0.12), p.accent.Alpha(0.2), 0.95)
					b.Children(func() { ui.Text(c, "新版本").FontSize(10.5).FontWeight(600).TextColor(p.accent).SingleLine() })
					if b.Clicked() {
						app.upd.Open = true
					}
				}
			})
		}
		if runtime.GOOS != "darwin" { // a hidden title bar hides the menu bar
			b := ui.ButtonBase(c.Key("updates")).Padding(4, 10).Justify(ui.Start).FocusRing(false).Label("检查更新")
			b.Children(func() { ui.Text(c, "检查更新").FontSize(10.5).TextColor(ternary(b.Hovered(), p.muted, p.faint)) })
			if b.Clicked() {
				go app.checkForUpdates(true)
			}
		}
	})
}

func (app *App) navSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(app.pal.faint).Padding(14, 10, 6).Role(ui.RoleHeading).Level(2)
}

func (app *App) navItem(c *ui.Context, icon *ui.SVG, label, page string, count int) {
	p := app.pal
	selected := app.Page == page && len(app.Stack) == 0 && !app.Dev.LogOpen
	b := ui.ButtonBase(c.Key(page)).Height(34).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label(label)
	if selected {
		b.Current(ui.CurrentPage)
	}
	if count > 0 {
		b.Description(fmt.Sprintf("%d部", count))
	}
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.press
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, quick))
	color := p.muted
	if selected {
		color = p.accent
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17).TextColor(b.AnimateColor("icon", color, quick))
		t := ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.text).Grow(1)
		if selected {
			t.FontWeight(600)
		}
		if count > 0 {
			ui.Text(c, itoa(count)).FontSize(11).TextColor(p.faint)
		}
	})
	if b.Clicked() {
		app.Dev.LogOpen = false
		app.Go(page)
	}
}

func (app *App) content(c *ui.Context) {
	if app.InitErr != "" {
		app.centered(c, iconX, "启动失败", app.InitErr, nil)
		return
	}
	if app.Dev.LogOpen {
		app.logPage(c)
		return
	}
	if n := len(app.Stack); n > 0 {
		app.collectionPage(c, app.Stack[n-1], true)
		return
	}
	switch app.Page {
	case core.PageSources:
		app.sourcesPage(c)
		return
	case core.PageSearch:
		app.searchPage(c)
		return
	case core.PageHistory:
		app.historyPage(c)
		return
	case core.PageFollows:
		app.followsPage(c)
		return
	}
	switch {
	case app.Src == nil:
		app.guidePage(c)
	case app.Page != "":
		app.collectionPage(c, app.Col(app.Page), false)
	case app.NavErr != "":
		app.centered(c, iconX, "片源加载失败", app.NavErr, func() { app.NavErr = ""; app.LoadNav() })
	default:
		app.skeletonGrid(c, 3)
	}
}

// hideWindow takes the window off the screen and leaves the app running.
// A full screen window leaves full screen first, or its space stays behind
// empty.
func (app *App) hideWindow() {
	if app.win.IsFullScreen() {
		app.hideOnLeave = true
		app.win.SetFullScreen(false)
		return
	}
	app.widen()
	app.win.Hide()
}

// Run starts the app and returns when it quits.
// Run starts the app and returns when it quits.
func Run() {
	app := &App{}
	mygo.App.WhenReady(func() {
		dataDir, _ := mygo.App.Path(mygo.PathUserData)
		cacheDir, _ := mygo.App.Path(mygo.PathCache)
		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "一集",
			Width:                1280,
			Height:               820,
			MinWidth:             core.MinWindowWidth,
			MinHeight:            core.MinWindowHeight,
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			StateKey:             "main",
			Content: ui.View(func(c *ui.Context) {
				if app.App != nil {
					app.view(c)
				}
			}),
		})
		app.posters = newPosters(cacheDir, app.win.Invalidate)
		app.App = core.New(core.Config{
			DataDir:  dataDir,
			CacheDir: cacheDir,
			Version:  mygo.App.Version(),
			Locale:   userLocale(),
			Host:     app,
		})
		app.posters.route = app.Sources.ImageRoute
		app.setMenu()
		app.mediaKeys()
		app.win.OnClose(func(e *mygo.CloseEvent) {
			// Only macOS has a Dock icon to bring the window back.
			if !app.quitting && runtime.GOOS == "darwin" {
				e.PreventDefault()
				app.CloseWatch()
				app.Lib.Flush()
				app.hideWindow()
				return
			}
			if app.Watch != nil {
				app.Watch.Close()
			}
			app.widen()
			app.Lib.Flush()
		})
		app.win.OnLeaveFullScreen(func() {
			if app.hideOnLeave {
				app.hideOnLeave = false
				app.hideWindow()
			}
		})
		// The Dock icon brings back the window closing hid.
		mygo.App.OnActivate(func(visible bool) {
			if !visible && !app.win.IsMinimized() {
				app.win.Show()
			}
		})
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { app.quitting = true })
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.widen(); app.Lib.Close(); app.SaveFeeds(true) })
		// Saves progress now and then, in case the app is killed.
		go func() {
			for range time.Tick(10 * time.Second) {
				mygo.RunOnMain(func() { app.Flush(false) })
			}
		}()
		go app.watchUpdates()
		app.tour()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
