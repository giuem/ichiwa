// Package app is the window of the app: its pages, the player, the menu
// bar and the media keys, in MyGo's native UI.
package app

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/media"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/sources/hongguo"
)

const sidebarWidth = 220

//go:embed assets/logo.png
var logoPNG []byte

var logo, _ = ui.DecodeBitmap(logoPNG)

// Page names.
const (
	pageRecommend = hongguo.ColRecommend
	pageLive      = hongguo.ColLive
	pageComic     = hongguo.ColComic
	pageRank      = hongguo.ColRank
	pageFilter    = hongguo.ColFilter
	pageSearch    = "search"
	pageHistory   = "history"
	pageFollows   = "follows"
)

// App is the state the window shows.
type App struct {
	win     *mygo.Window
	src     source.Source // the source shown
	sources registry      // every source loaded
	// srcCtx is canceled when the app leaves the source shown, pageCtx
	// when it leaves the page shown (§9): a call they canceled is dropped
	// without an error. Both are made when first asked for.
	srcCtx, pageCtx   context.Context
	srcStop, pageStop context.CancelFunc
	// locale is the user's language, for plugins; "" in tests.
	locale  string
	dev     devState
	media   *media.Manager
	nav     []source.NavItem
	navErr  string
	navBusy bool
	// cols are the collection pages of a plugin, by collection ID.
	cols map[string]*colState
	// dataDir keeps what the app must not lose.
	dataDir string
	posters *Posters
	lib     *library.Library
	// cacheDir keeps what the app may lose: covers, episodes, feeds.
	cacheDir string
	initErr  string
	pal      palette

	page  string
	feeds map[string]*Feed

	rank   rankState
	filter filterState
	search searchState
	shelf  ui.ScrollState
	// historyFilter is the history page's filter, in historyFilters.
	historyFilter int

	// person is an actor's page, over the sidebar's page while it is open.
	person *personState

	// narrowed tells the window gave up the width of the folded panel.
	narrowed bool
	// quitting tells the window to really close, not just hide.
	quitting bool
	// hideOnLeave hides the window once it has left full screen.
	hideOnLeave bool
	// narrowedBy is the width it gave up.
	narrowedBy int
	// watch is the player page, over the rest while it is open.
	watch *Watching
	toast string
	// unmute is the volume the mute button goes back to.
	unmute float64

	// miniOn asks for the small window; mini tells the window is it, and
	// miniBack is the size it goes back to.
	miniOn, mini         bool
	miniBackW, miniBackH int
	miniW, miniH         int
	miniFull             bool // the small window is in full screen
	miniFixUntil         time.Time

	// The sleep timer, which outlasts a season going on to the next:
	// sleepMins is the choice, sleepEpisode for the end of this episode,
	// and sleepAt when the time runs out.
	sleepMins int
	sleepAt   time.Time

	// told is the page title screen readers were last told of.
	told string

	warmed bool // the first pages of the other pages were asked for

	// upd is a newer version found, which the sidebar marks, and its
	// dialog.
	upd updateState

	// post schedules callbacks onto the UI thread; defaults to app.win.Update.
	post func(func())
}

func (app *App) dispatch(fn func()) {
	if app.post != nil {
		app.post(fn)
		return
	}
	if app.win != nil {
		app.win.Update(fn)
	}
}

func (app *App) sourceID() string {
	if app.src != nil {
		return app.src.Info().ID
	}
	return library.LegacySource
}

func (app *App) hasCap(c source.Capability) bool {
	return app.src != nil && app.src.Info().Has(c)
}

// async runs work on a goroutine and then done with its result on the main
// thread, redrawing the window.
func async[T any](app *App, work func() (T, error), done func(T, error)) {
	go func() {
		v, err := work()
		app.dispatch(func() { done(v, err) })
	}()
}

// srcContext is the context of the calls for the source shown.
func (app *App) srcContext() context.Context {
	if app.srcCtx == nil {
		app.srcCtx, app.srcStop = context.WithCancel(context.Background())
	}
	return app.srcCtx
}

// pageContext is the context of the calls for the page shown.
func (app *App) pageContext() context.Context {
	if app.pageCtx == nil {
		app.pageCtx, app.pageStop = context.WithCancel(app.srcContext())
	}
	return app.pageCtx
}

// leavePage cancels the calls for the page shown.
func (app *App) leavePage() {
	if app.pageStop != nil {
		app.pageStop()
	}
	app.pageCtx, app.pageStop = nil, nil
}

// leaveSource cancels the calls for the source shown and its pages.
func (app *App) leaveSource() {
	app.leavePage()
	if app.srcStop != nil {
		app.srcStop()
	}
	app.srcCtx, app.srcStop = nil, nil
}

// go2 shows a page of the sidebar.
func (app *App) go2(page string) {
	if page != app.page || app.person != nil {
		app.leavePage()
	}
	app.page = page
	app.person = nil
	app.lib.Page = page
	app.lib.MarkDirty()
	if app.watch != nil {
		app.closeWatch()
	}
}

// The smallest the window may be: the width takes the player's controls,
// the height the whole sidebar.
const (
	minWindowWidth  = 480
	minWindowHeight = 480
)

func (app *App) view(c *ui.Context) {
	app.syncMini()
	app.syncNarrow()
	if c.Theme().Dark || app.watch != nil {
		app.pal = darkPalette()
	} else {
		app.pal = lightPalette()
	}
	if c.Preferences().HighContrast {
		app.pal = app.pal.contrasted()
	}
	applyPalette(c, app.pal)
	if app.toast != "" {
		c.Toast(app.toast)
		app.toast = ""
	}

	app.updateDialog(c)
	if app.watch != nil {
		app.watchView(c)
		return
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	if app.hasCap(source.CapSearch) {
		c.OnShortcut(ui.Cmd, ui.KeyF, func() { app.go2(pageSearch); app.search.focus = true })
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
	app.lib.Flush()
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
		app.navSection(c, "发现")
		for _, item := range app.nav {
			app.navItem(c, navIcon(item.Icon), item.Title, item.ID, 0)
		}
		if app.hasCap(source.CapSearch) {
			app.navItem(c, iconSearch, "搜索", pageSearch, 0)
		}
		app.navSection(c, "我的")
		app.navItem(c, iconClock, "继续观看", pageHistory, len(app.lib.History))
		app.navItem(c, iconHeart, "追剧", pageFollows, len(app.lib.Follows))
		if app.dev.plugin != nil {
			b := ui.ButtonBase(c.Key("plugin-log")).Height(34).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Label("插件日志")
			if app.dev.logOpen {
				b.Current(ui.CurrentPage).Background(p.press)
			}
			b.Children(func() {
				ui.Icon(c, iconScroll).Size(17, 17).TextColor(p.muted)
				ui.Text(c, "插件日志").FontSize(13.5).FontWeight(500).TextColor(p.text)
			})
			if b.Clicked() {
				app.dev.logOpen = true
				app.closePerson()
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
						app.upd.open = true
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
	selected := app.page == page && app.person == nil && !app.dev.logOpen
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
		app.dev.logOpen = false
		app.go2(page)
	}
}

func (app *App) content(c *ui.Context) {
	if app.initErr != "" {
		app.centered(c, iconX, "启动失败", app.initErr, nil)
		return
	}
	if app.dev.logOpen {
		app.logPage(c)
		return
	}
	if app.person != nil {
		app.personPage(c)
		return
	}
	switch app.page {
	case pageSearch:
		app.searchPage(c)
		return
	case pageHistory:
		app.historyPage(c)
		return
	case pageFollows:
		app.followsPage(c)
		return
	}
	if !app.builtin() {
		switch {
		case app.page != "":
			app.collectionPage(c, app.page)
		case app.navErr != "":
			app.centered(c, iconX, "片源加载失败", app.navErr, func() { app.navErr = ""; app.loadNav() })
		default:
			app.skeletonGrid(c, 3)
		}
		return
	}
	switch app.page {
	case pageRank:
		app.rankPage(c)
	case pageFilter:
		app.filterPage(c)
	default:
		app.feedPage(c, app.page)
	}
}

func normalizeSavedPage(saved string) string {
	switch saved {
	case "推荐":
		return pageRecommend
	case "真人剧":
		return pageLive
	case "漫剧":
		return pageComic
	case pageRecommend, pageLive, pageComic, pageRank, pageFilter, pageSearch, pageHistory, pageFollows:
		return saved
	default:
		return pageRecommend
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
func Run() {
	app := &App{page: pageRecommend, feeds: map[string]*Feed{}}
	mygo.App.WhenReady(func() {
		dataDir, _ := mygo.App.Path(mygo.PathUserData)
		cacheDir, _ := mygo.App.Path(mygo.PathCache)
		app.cacheDir = cacheDir
		app.lib = library.Load(dataDir)
		app.page = normalizeSavedPage(app.lib.Page)

		app.dataDir = dataDir
		app.locale = userLocale()
		src, err := hongguo.New(dataDir)
		if err != nil {
			app.initErr = err.Error()
		} else {
			app.src = src
			app.sources.add(src)
			if nav, nerr := src.Navigation(context.Background()); nerr == nil {
				app.nav = nav
			}
		}
		if app.initErr == "" {
			mgr, merr := media.New(cacheDir)
			if merr != nil {
				app.initErr = merr.Error()
			} else {
				app.media = mgr
				mgr.SetPolicy(app.sources.mediaPolicy)
			}
		}

		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "一集",
			Width:                1280,
			Height:               820,
			MinWidth:             minWindowWidth,
			MinHeight:            minWindowHeight,
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			StateKey:             "main",
			Content:              ui.View(app.view),
		})
		app.posters = newPosters(cacheDir, app.win.Invalidate)
		app.posters.route = app.sources.imageRoute
		// The developer mode's plugin folder, loaded at the last launch.
		if dev := app.loadDevSettings(); dev.Dir != "" && app.initErr == "" {
			if err := app.loadDev(dev.Dir); err != nil {
				log.Printf("plugins: %s: %v", dev.Dir, err)
			} else if os.Getenv("ICHIWA_PLUGIN_DIR") != "" || dev.Current == app.dev.plugin.Info().ID {
				app.useSource(app.dev.plugin)
			}
		}
		app.setMenu()
		app.mediaKeys()
		app.win.OnClose(func(e *mygo.CloseEvent) {
			// Only macOS has a Dock icon to bring the window back.
			if !app.quitting && runtime.GOOS == "darwin" {
				e.PreventDefault()
				app.closeWatch()
				app.lib.Flush()
				app.hideWindow()
				return
			}
			if app.watch != nil {
				app.watch.close()
			}
			app.widen()
			app.lib.Flush()
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
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.widen(); app.lib.Close(); app.saveFeeds(true) })
		// Saves progress now and then, in case the app is killed.
		go func() {
			for range time.Tick(10 * time.Second) {
				mygo.RunOnMain(func() { app.lib.Flush(); app.saveFeeds(false) })
			}
		}()
		go app.watchUpdates()
		app.tour()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
