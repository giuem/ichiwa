// Package app is the window of the app: its pages, the player, the menu
// bar and the media keys, in MyGo's native UI.
package app

import (
	_ "embed"
	"log"

	"hongguo/internal/catalog"
	"hongguo/internal/library"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const sidebarWidth = 220

//go:embed assets/logo.png
var logoPNG []byte

var logo, _ = ui.DecodeBitmap(logoPNG)

// Page names.
const (
	pageRecommend = "推荐"
	pageLive      = "真人剧"
	pageComic     = "漫剧"
	pageRank      = "rank"
	pageSearch    = "search"
	pageHistory   = "history"
	pageFollows   = "follows"
)

// App is the state the window shows.
type App struct {
	win     *mygo.Window
	be      *catalog.Backend
	posters *Posters
	lib     *library.Library
	initErr string
	pal     palette

	page  string
	feeds map[string]*Feed

	rank   rankState
	search searchState
	shelf  ui.ScrollState

	// watch is the player page, over the rest while it is open.
	watch *Watching
	toast string

	warmed bool // the first pages of the other pages were asked for
}

// async runs work on a goroutine and then done with its result on the main
// thread, redrawing the window.
func async[T any](app *App, work func() (T, error), done func(T, error)) {
	go func() {
		v, err := work()
		app.win.Update(func() { done(v, err) })
	}()
}

// go2 shows a page of the sidebar.
func (app *App) go2(page string) {
	app.page = page
	app.lib.Page = page
	app.lib.MarkDirty()
	if app.watch != nil {
		app.closeWatch()
	}
}

func (app *App) view(c *ui.Context) {
	if c.Theme().Dark || app.watch != nil {
		app.pal = darkPalette()
	} else {
		app.pal = lightPalette()
	}
	applyPalette(c, app.pal)
	if app.toast != "" {
		c.Toast(app.toast)
		app.toast = ""
	}

	if app.watch != nil {
		app.watchView(c)
		return
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { app.go2(pageSearch); app.search.focus = true })
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0)
		if !c.Vibrancy() {
			side.Background(app.pal.sidebar)
		}
		side.Children(func() { app.sidebar(c) })
		ui.Column(c.Key("main")).Grow(1).Background(app.pal.bg).Children(func() {
			app.content(c)
		})
	})
	app.lib.Flush()
}

func (app *App) sidebar(c *ui.Context) {
	p := app.pal
	top := max(c.TitleBar().Height, 28) + 10
	ui.Column(c).Fill().Padding(top, 12, 14).Gap(2).DragWindow().Children(func() {
		ui.Row(c).Padding(2, 8, 12).Gap(10).Children(func() {
			ui.Image(c, logo).Size(36, 36).Margin(-3, -3, -3, -3)
			ui.Column(c).Children(func() {
				ui.Text(c, "红果短剧").FontSize(15).Bold().TextColor(p.text)
				ui.Text(c, "for Mac").FontSize(10.5).TextColor(p.muted)
			})
		})
		app.navSection(c, "发现")
		app.navItem(c, iconHome, "推荐", pageRecommend, 0)
		app.navItem(c, iconFilm, "真人剧", pageLive, 0)
		app.navItem(c, iconSparkles, "漫剧", pageComic, 0)
		app.navItem(c, iconTrophy, "排行榜", pageRank, 0)
		app.navItem(c, iconSearch, "搜索", pageSearch, 0)
		app.navSection(c, "我的")
		app.navItem(c, iconClock, "继续观看", pageHistory, len(app.lib.History))
		app.navItem(c, iconHeart, "追剧", pageFollows, len(app.lib.Follows))
		ui.Spacer(c)
		if v := mygo.App.Version(); v != "" {
			ui.Text(c, "v"+v).FontSize(10.5).TextColor(p.faint).Padding(0, 10)
		}
	})
}

func (app *App) navSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(app.pal.faint).Padding(14, 10, 6)
}

func (app *App) navItem(c *ui.Context, icon *ui.SVG, label, page string, count int) {
	p := app.pal
	selected := app.page == page
	b := ui.ButtonBase(c.Key(page)).Height(34).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(label)
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
		app.go2(page)
	}
}

func (app *App) content(c *ui.Context) {
	if app.initErr != "" {
		app.centered(c, iconX, "启动失败", app.initErr, nil)
		return
	}
	switch app.page {
	case pageRank:
		app.rankPage(c)
	case pageSearch:
		app.searchPage(c)
	case pageHistory:
		app.historyPage(c)
	case pageFollows:
		app.followsPage(c)
	default:
		app.feedPage(c, app.page)
	}
}

// Run starts the app and returns when it quits.
func Run() {
	app := &App{page: pageRecommend, feeds: map[string]*Feed{}}
	mygo.App.WhenReady(func() {
		dataDir, _ := mygo.App.Path(mygo.PathUserData)
		cacheDir, _ := mygo.App.Path(mygo.PathCache)
		app.lib = library.Load(dataDir)
		switch app.lib.Page {
		case pageRecommend, pageLive, pageComic, pageRank, pageSearch, pageHistory, pageFollows:
			app.page = app.lib.Page
		}
		be, err := catalog.New(dataDir, cacheDir)
		if err != nil {
			app.initErr = err.Error()
		}
		app.be = be
		app.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "红果短剧",
			Width:                1280,
			Height:               820,
			MinWidth:             980,
			MinHeight:            640,
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			StateKey:             "main",
			Content:              ui.View(app.view),
		})
		app.posters = newPosters(cacheDir, app.win.Invalidate)
		app.setMenu()
		app.mediaKeys()
		app.win.OnClose(func(*mygo.CloseEvent) {
			if app.watch != nil {
				app.watch.close()
			}
			app.lib.Flush()
		})
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.lib.Close() })
		// Saves progress now and then, in case the app is killed.
		go func() {
			for range time.Tick(10 * time.Second) {
				mygo.RunOnMain(app.lib.Flush)
			}
		}()
		go func() {
			time.Sleep(5 * time.Second)
			checkForUpdates(false)
		}()
		app.tour()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
