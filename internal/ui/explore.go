package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// avatar draws a round portrait, fading it in over the first letter of
// the name as it loads.
func (app *App) avatar(c *ui.Context, url, name string, size float32) ui.Element {
	p := app.pal
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(999).Clip()
	bmp := app.posters.Get(url)
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(p.surface.Mix(p.text, 0.06))
	}
	box.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Opacity(1 - in).AccessHidden(true).Children(func() {
				ui.Text(c, firstRune(name)).FontSize(size * 0.4).Bold().TextColor(p.muted)
			})
		}
		if bmp != nil {
			img := ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Label(name)
			if in < 1 {
				img.Opacity(in)
			}
		}
	})
	return box
}

// option is a choice in a row of the filter panel.
func (app *App) option(c *ui.Context, label string, selected bool) ui.Element {
	p := app.pal
	b := radio(ui.ButtonBase(c).Height(28).PaddingX(11).Radius(7).Center().Label(label), selected)
	rest, fg := ui.Transparent, p.muted
	if selected {
		rest, fg = p.accent.Alpha(0.12), p.accent
	}
	pressable(b, rest, ternary(selected, rest, p.hover), 0.95)
	b.Children(func() {
		t := ui.Text(c, label).FontSize(12.5).SingleLine().TextColor(b.AnimateColor("fg", fg, quick))
		if selected {
			t.FontWeight(600)
		}
	})
	return b
}

// ---- Actors ----

const actorCardMinW = 300

// actorGrid lays out people in rows of cards, with their places when
// ranked.
func (app *App) actorGrid(c *ui.Context, actors []Card, ranked bool) {
	w := app.contentWidth(c)
	n := max(1, int((w+cardGap)/(actorCardMinW+cardGap)))
	cw := (w - cardGap*float32(n-1)) / float32(n)
	ui.Column(c).Gap(14).Children(func() {
		for start := 0; start < len(actors); start += n {
			ui.Row(c.Key(start)).Gap(cardGap).AlignItems(ui.Stretch).Children(func() {
				for i := start; i < min(start+n, len(actors)); i++ {
					app.actorCard(c, i, actors[i], cw, ranked)
				}
			})
		}
	})
}

func (app *App) actorCard(c *ui.Context, i int, a Card, width float32, ranked bool) {
	p := app.pal
	b := ui.ButtonBase(c.Key(a.ID)).Width(width).Padding(14, 16).Gap(14).Radius(14).
		AlignItems(ui.Center).Justify(ui.Start).Label(a.Title).
		Description(joinNonEmpty(ternary(ranked, fmt.Sprintf("第%d名", i+1), ""), a.Note, a.Subtitle))
	pressable(b, p.surface, p.surface.Mix(p.text, 0.05), 0.98)
	b.Children(func() {
		if ranked {
			ui.Text(c, itoa(i+1)).FontSize(15).Bold().Width(22).TextAlign(ui.Center).TextColor(ternary(i < 3, p.accent, p.faint))
		}
		app.avatar(c, a.Image, a.Title, 56)
		ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
			ui.Text(c, a.Title).FontSize(14.5).FontWeight(600).TextColor(p.text).SingleLine()
			if a.Note != "" {
				ui.Text(c, a.Note).FontSize(12).FontWeight(500).TextColor(p.accent).SingleLine()
			}
			if a.Subtitle != "" {
				ui.Text(c, a.Subtitle).FontSize(11.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	b.OnClick(func() { app.openCard(app.SourceID(), a) })
}
