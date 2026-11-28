package ui

import (
	"github.com/egoist/mygo/ui"
)

// pump loads the next page of f, shown in a scroll container of the view
// c builds, while less than about two screens are loaded below the view.
// That covers a feed too short to fill the view, whose MaxY is 0.
func (app *App) pump(c *ui.Context, f *Feed) {
	if !f.More || f.Loading || f.Err != "" {
		return
	}
	_, h := c.Size()
	if scrollOf(f).MaxY-scrollOf(f).Y < 2*h {
		app.LoadMore(f)
	}
}
