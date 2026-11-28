package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/sources/hongguo"
)

// tour walks the pages and saves a screenshot of each into the directory
// ICHIWA_TOUR names, then quits. It is for checking the interface.
func (app *App) tour() {
	dir := os.Getenv("ICHIWA_TOUR")
	if dir == "" {
		return
	}
	go func() {
		shot := func(name string, wait time.Duration) {
			time.Sleep(wait)
			png, err := app.win.CapturePage()
			if err != nil {
				fmt.Fprintln(os.Stderr, "capture:", err)
				return
			}
			_ = os.WriteFile(filepath.Join(dir, name+".png"), png, 0o644)
			fmt.Fprintln(os.Stderr, "shot", name)
		}
		goTo := func(page string) { app.win.Update(func() { app.go2(page) }) }
		hg := app.sourceID() == hongguo.SourceID
		query, filterKey, filterValue, actors := "兔子", "genre", "奇幻", ""
		if hg {
			query, filterValue, actors = "总裁", "comic_series", "ranklist_celebrity"
		}
		var nav []source.NavItem
		mygo.RunOnMain(func() { nav = app.nav })
		shot("01-home", 8*time.Second)
		for i, it := range nav[1:] {
			goTo(it.ID)
			shot(fmt.Sprintf("%02d-nav-%d", i+2, i+2), 6*time.Second)
		}
		goTo(pageSearch)
		shot("10-search", 5*time.Second)
		app.win.Update(func() { app.search.query = query[:3]; app.typed(query[:3]) })
		shot("11-suggest", 3*time.Second)
		app.win.Update(func() { app.search.query = query; app.runSearch(query) })
		shot("12-results", 6*time.Second)
		app.win.Update(func() {
			if cs := app.col(nav[0].ID); len(cs.results.items) > 0 {
				app.lib.Danmaku = true
				app.open(app.sourceID(), cs.results.items[0], -1)
			}
		})
		shot("13-player", 15*time.Second)
		app.win.Update(func() {
			if app.watch != nil {
				app.watch.togglePlay()
			}
		})
		shot("14-player-paused", 2*time.Second)
		app.win.Update(func() { app.closeWatch(); app.go2(pageHistory) })
		shot("15-history", 2*time.Second)
		goTo(pageFollows)
		shot("16-follows", 2*time.Second)
		goTo("filter")
		shot("17-filter", 6*time.Second)
		app.win.Update(func() { app.col("filter").choose(filterKey, filterValue) })
		shot("18-filter-chosen", 6*time.Second)
		if !hg {
			app.win.Update(func() { app.col("filter").choose(filterKey, "broken") })
			shot("19-filter-broken", 3*time.Second)
		}
		goTo("rank")
		if actors != "" {
			app.win.Update(func() { app.col("rank").choose("content", actors) })
		}
		shot("20-rank", 6*time.Second)
		app.win.Update(func() {
			for _, it := range app.col("rank").results.items {
				if it.Kind == source.CardCollection {
					app.openCollection(it)
					return
				}
			}
			app.openCollection(Card{Kind: source.CardCollection, ID: "person:p1", Title: "大兔子"})
		})
		shot("21-person", 6*time.Second)
		if !hg {
			app.win.Update(func() {
				app.open(app.sourceID(), Card{Kind: source.CardMedia, ID: "bipbop", Title: "测试信号"}, 0)
			})
			shot("22-player-hls", 12*time.Second)
			app.win.Update(func() { app.closeWatch(); app.dev.logOpen = true })
			shot("23-log", 2*time.Second)
		}
		mygo.RunOnMain(func() { mygo.App.Quit() })
	}()
}
