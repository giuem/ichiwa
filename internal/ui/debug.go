package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	core "github.com/giuem/ichiwa/internal/app"

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
		goTo := func(page string) { app.win.Update(func() { app.Go(page) }) }
		hg := app.SourceID() == hongguo.SourceID
		query, filterKey, filterValue, actors := "兔子", "genre", "奇幻", ""
		if hg {
			query, filterValue, actors = "总裁", "comic_series", "ranklist_celebrity"
		}
		shot("01-home", 8*time.Second)
		var nav []source.NavItem
		mygo.RunOnMain(func() { nav = app.Nav })
		for i, it := range nav[min(1, len(nav)):] {
			goTo(it.ID)
			shot(fmt.Sprintf("%02d-nav-%d", i+2, i+2), 6*time.Second)
		}
		goTo(core.PageSearch)
		shot("10-search", 5*time.Second)
		app.win.Update(func() { app.Search.Query = query[:3]; app.Typed(query[:3]) })
		shot("11-suggest", 3*time.Second)
		app.win.Update(func() { app.Search.Query = query; app.RunSearch(query) })
		shot("12-results", 6*time.Second)
		app.win.Update(func() {
			if cs := app.Col(nav[0].ID); len(cs.Results.Items) > 0 {
				app.Lib.Danmaku = true
				app.Open(app.SourceID(), cs.Results.Items[0], -1)
			}
		})
		shot("13-player", 15*time.Second)
		app.win.Update(func() {
			if app.Watch != nil {
				app.Watch.TogglePlay()
			}
		})
		shot("14-player-paused", 2*time.Second)
		app.win.Update(func() { app.CloseWatch(); app.Go(core.PageHistory) })
		shot("15-history", 2*time.Second)
		goTo(core.PageFollows)
		shot("16-follows", 2*time.Second)
		goTo("filter")
		shot("17-filter", 6*time.Second)
		app.win.Update(func() { app.Col("filter").Choose(filterKey, filterValue) })
		shot("18-filter-chosen", 6*time.Second)
		if !hg {
			app.win.Update(func() { app.Col("filter").Choose(filterKey, "broken") })
			shot("19-filter-broken", 3*time.Second)
		}
		goTo("rank")
		if actors != "" {
			app.win.Update(func() { app.Col("rank").Choose("content", actors) })
		}
		shot("20-rank", 6*time.Second)
		app.win.Update(func() {
			for _, it := range app.Col("rank").Results.Items {
				if it.Kind == source.CardCollection {
					app.OpenCollection(it)
					return
				}
			}
			app.OpenCollection(Card{Kind: source.CardCollection, ID: "person:p1", Title: "大兔子"})
		})
		shot("21-person", 6*time.Second)
		if !hg {
			app.win.Update(func() {
				app.Open(app.SourceID(), Card{Kind: source.CardMedia, ID: "bipbop", Title: "测试信号"}, 0)
			})
			shot("22-player-hls", 12*time.Second)
			app.win.Update(func() { app.CloseWatch(); app.Go(core.PageSources); app.LogOf = app.SourceID() })
			shot("23-log", 2*time.Second)
		}
		app.win.Update(func() { app.CloseWatch(); app.Go(core.PageSources) })
		shot("30-sources", 2*time.Second)
		app.win.Update(func() {
			for _, s := range app.SourcesOn() {
				if s != app.Src {
					app.SwitchSource(s)
					return
				}
			}
		})
		shot("31-switched", 8*time.Second)
		app.win.Update(func() {
			for _, s := range app.Sources.All() {
				app.SetSourceOn(s.Info().ID, false)
			}
			app.Go("")
		})
		shot("32-no-source", 2*time.Second)
		app.win.Update(func() {
			for _, s := range app.Sources.All() {
				app.SetSourceOn(s.Info().ID, true)
			}
		})
		mygo.RunOnMain(func() { mygo.App.Quit() })
	}()
}
