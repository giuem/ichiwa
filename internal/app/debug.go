package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

// tour walks the pages and saves a screenshot of each into the directory
// HONGGUO_TOUR names, then quits. It is for checking the interface.
func (app *App) tour() {
	dir := os.Getenv("HONGGUO_TOUR")
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
		goTo := func(page string) { app.win.Update(func() { app.page = page }) }
		shot("1-home", 8*time.Second)
		goTo(pageComic)
		shot("2-comic", 6*time.Second)
		goTo(pageRank)
		shot("3-rank", 6*time.Second)
		goTo(pageSearch)
		shot("4-search", 5*time.Second)
		app.win.Update(func() { app.search.query = "总裁"; app.runSearch("总裁", false) })
		shot("5-results", 6*time.Second)
		app.win.Update(func() {
			if f := app.feeds[pageRecommend]; f != nil && len(f.items) > 0 {
				app.open(f.items[0], -1)
			}
		})
		shot("6-player", 15*time.Second)
		app.win.Update(func() { app.watch.togglePlay() })
		shot("7-player-paused", 2*time.Second)
		app.win.Update(func() { app.closeWatch(); app.page = pageHistory })
		shot("8-history", 2*time.Second)
		mygo.RunOnMain(func() { mygo.App.Quit() })
	}()
}
