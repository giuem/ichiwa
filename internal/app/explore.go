package app

import (
	"fmt"
	"maps"
	"strings"

	"github.com/egoist/mygo/ui"

	"hongguo/internal/catalog"
)

// pages adapts a catalog call returning a page of shows to a feed's fetch.
func pages(fetch func(cursor string) ([]Show, string, bool, error)) func(string) (feedPage, error) {
	return func(cursor string) (feedPage, error) {
		items, next, more, err := fetch(cursor)
		return feedPage{items: items, cursor: next, more: more}, err
	}
}

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

// ---- Filter ----

type filterState struct {
	rows      []FilterRow
	rowsFor   string // the genre the rows are for
	rowsOK    bool
	panelBusy bool
	panelErr  string

	chosen  map[string]string // a value per row key, none for all
	results Feed              // for chosen; without a fetch until shown
}

// choose picks value in the row key, "" for all, and reports whether the
// choice changed. The genre decides the other rows' options, so picking
// one clears the rest.
func (fs *filterState) choose(key, value string) bool {
	if fs.chosen[key] == value {
		return false
	}
	if key == catalog.GenreKey || fs.chosen == nil {
		fs.chosen = map[string]string{}
	}
	if value == "" {
		delete(fs.chosen, key)
	} else {
		fs.chosen[key] = value
	}
	fs.restart()
	return true
}

// clear goes back to all dramas.
func (fs *filterState) clear() {
	fs.chosen = nil
	fs.restart()
}

// restart drops the results, and loads in flight, for a new choice. The
// view gives the feed a fetch for it.
func (fs *filterState) restart() {
	fs.results.reset()
	fs.results.fetch = nil
}

func (app *App) loadFilterPanel() {
	fs := &app.filter
	genre := fs.chosen[catalog.GenreKey]
	fs.panelBusy, fs.panelErr = true, ""
	async(app, func() ([]FilterRow, error) { return app.be.FilterPanel(genre) }, func(rows []FilterRow, err error) {
		fs.panelBusy = false
		if fs.chosen[catalog.GenreKey] != genre {
			return // the genre changed meanwhile; the view asks again
		}
		if err != nil {
			fs.panelErr = errText(err)
			return
		}
		fs.rows, fs.rowsFor, fs.rowsOK = rows, genre, true
	})
}

func (app *App) filterPage(c *ui.Context) {
	fs := &app.filter
	p := app.pal
	genre := fs.chosen[catalog.GenreKey]
	if !fs.panelBusy && fs.panelErr == "" && (!fs.rowsOK || fs.rowsFor != genre) {
		app.loadFilterPanel()
	}
	r := &fs.results
	if r.fetch == nil {
		chosen := maps.Clone(fs.chosen) // the fetch runs off the main thread
		r.fetch = pages(func(cursor string) ([]Show, string, bool, error) { return app.be.Filter(chosen, cursor) })
		r.more = !r.loaded
	}
	retry := func() { r.err = ""; app.loadMore(r) }
	app.header(c, "筛选", "按题材、设定找到想看的剧", func() {
		if len(fs.chosen) > 0 {
			app.secondary(c, iconRefresh, "重置").OnClick(fs.clear)
		}
	})
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("filter")).TrackScroll(&r.scroll).Grow(1).Padding(0, pagePad, 0).Children(func() {
		app.filterPanel(c)
		ui.Box(c).Height(1).Background(p.hairline).Margin(14, 0, 22)
		switch {
		case len(r.items) > 0:
			app.grid(c, visible(r, n), episodesExtras)
			app.feedFooter(c, r.loading, r.err, r.more, retry)
		case r.loaded && !r.more && r.err == "":
			ui.Text(c, "没有符合条件的短剧，换个条件试试").FontSize(13).TextColor(p.muted).Padding(40, 0).AlignSelf(ui.Center)
		default:
			app.feedFooter(c, r.err == "", r.err, true, retry)
		}
	})
	app.pump(c, r)
}

// filterPanel draws a row of options for each of the panel's rows.
func (app *App) filterPanel(c *ui.Context) {
	fs := &app.filter
	p := app.pal
	if !fs.rowsOK {
		if fs.panelErr != "" {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(4, 0).Children(func() {
				ui.Text(c, "筛选条件加载失败："+fs.panelErr).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
				app.secondary(c, iconRefresh, "重试").OnClick(func() { fs.panelErr = "" })
			})
			return
		}
		sh := app.shimmer(c)
		ui.Column(c).Gap(12).Role(ui.RoleStatus).Label("加载中").Children(func() {
			for i := range 4 {
				sh.bone(c.Key(i), 7).Height(28).WidthPercent(float32(92 - i*13))
			}
		})
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, row := range fs.rows {
			choices(c.Key("row-"+row.Key), row.Name).Gap(4).Wrap().Children(func() {
				all := fs.chosen[row.Key] == ""
				key := row.Key
				// It shows the row's name and chooses all of it.
				app.option(c.Key("all"), row.Name, all).Label("全部" + row.Name).OnClick(func() { fs.choose(key, "") })
				for _, o := range row.Options {
					value := o.Value
					app.option(c.Key(o.Value), o.Name, fs.chosen[key] == value).OnClick(func() { fs.choose(key, value) })
				}
			})
		}
	})
}

// ---- Actors ----

const actorCardMinW = 300

// actorGrid lays out the rows of the actors' ranking.
func (app *App) actorGrid(c *ui.Context, actors []Actor) {
	w := app.contentWidth(c)
	n := max(1, int((w+cardGap)/(actorCardMinW+cardGap)))
	cw := (w - cardGap*float32(n-1)) / float32(n)
	ui.Column(c).Gap(14).Children(func() {
		for start := 0; start < len(actors); start += n {
			ui.Row(c.Key(start)).Gap(cardGap).AlignItems(ui.Stretch).Children(func() {
				for i := start; i < min(start+n, len(actors)); i++ {
					app.actorCard(c, i, actors[i], cw)
				}
			})
		}
	})
}

func (app *App) actorCard(c *ui.Context, i int, a Actor, width float32) {
	p := app.pal
	b := ui.ButtonBase(c.Key(a.ID)).Width(width).Padding(14, 16).Gap(14).Radius(14).
		AlignItems(ui.Center).Justify(ui.Start).Label(a.Name).
		Description(joinNonEmpty(append([]string{fmt.Sprintf("第%d名", i+1), a.Score}, a.Details...)...))
	pressable(b, p.surface, p.surface.Mix(p.text, 0.05), 0.98)
	b.Children(func() {
		ui.Text(c, itoa(i+1)).FontSize(15).Bold().Width(22).TextAlign(ui.Center).TextColor(ternary(i < 3, p.accent, p.faint))
		app.avatar(c, a.Avatar, a.Name, 56)
		ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
			ui.Text(c, a.Name).FontSize(14.5).FontWeight(600).TextColor(p.text).SingleLine()
			if a.Score != "" {
				ui.Text(c, a.Score).FontSize(12).FontWeight(500).TextColor(p.accent).SingleLine()
			}
			if len(a.Details) > 0 {
				ui.Text(c, strings.Join(a.Details, " · ")).FontSize(11.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	b.OnClick(func() { app.openPerson(a) })
}

// ---- An actor's page ----

// personState is an actor's page, shown over the sidebar's page.
type personState struct {
	actor Actor // what the ranking knew, shown until the profile loads
	info  *Person
	busy  bool
	err   string
	works Feed
}

func (app *App) openPerson(a Actor) {
	ps := &personState{actor: a}
	ps.works = *newFeed(pages(func(cursor string) ([]Show, string, bool, error) { return app.be.Works(a.ID, cursor) }))
	app.person = ps
}

func (app *App) loadPerson(ps *personState) {
	ps.busy, ps.err = true, ""
	async(app, func() (*Person, error) { return app.be.Person(ps.actor.ID) }, func(info *Person, err error) {
		ps.busy = false
		if err != nil {
			ps.err = errText(err)
			return
		}
		ps.info = info
	})
}

func (app *App) personPage(c *ui.Context) {
	ps := app.person
	p := app.pal
	if ps.info == nil && !ps.busy && ps.err == "" {
		app.loadPerson(ps)
	}
	f := &ps.works
	retry := func() { f.err = ""; app.loadMore(f) }
	back := func() { app.person = nil }
	c.OnShortcut(0, ui.KeyEscape, back)

	name, avatar := ps.actor.Name, ps.actor.Avatar
	if ps.info != nil {
		name = ps.info.Name
		if ps.info.Avatar != "" {
			avatar = ps.info.Avatar
		}
	}
	app.tell(c, name)
	bar := c.TitleBar()
	ui.Row(c).Padding(max(bar.Height, 28)+4, pagePad-8, 6).DragWindow().Children(func() {
		iconButton(c, iconChevronLeft, 18, p.muted, "返回").OnClick(back)
	})
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("person-"+ps.actor.ID)).TrackScroll(&f.scroll).Grow(1).Padding(4, pagePad, 0).Children(func() {
		ui.Row(c).Gap(22).AlignItems(ui.Center).Children(func() {
			app.avatar(c, avatar, name, 96)
			ui.Column(c).Grow(1).Shrink(1).Gap(8).Children(func() {
				ui.Text(c, name).FontSize(26).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
				app.personFacts(c, ps)
			})
		})
		ui.Text(c, "作品").FontSize(17).Bold().TextColor(p.text).Margin(30, 0, 16).Role(ui.RoleHeading).Level(2)
		switch {
		case len(f.items) > 0:
			app.grid(c, visible(f, n), episodesExtras)
			app.feedFooter(c, f.loading, f.err, f.more, retry)
		case f.loaded && !f.more && f.err == "":
			ui.Text(c, "暂无作品").FontSize(13).TextColor(p.muted).Padding(24, 0)
		default:
			app.feedFooter(c, f.err == "", f.err, true, retry)
		}
	})
	app.pump(c, f)
}

// personFacts shows what the profile says under the name.
func (app *App) personFacts(c *ui.Context, ps *personState) {
	p := app.pal
	info := ps.info
	switch {
	case info == nil && ps.err != "":
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ps.err).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
			app.secondary(c, iconRefresh, "重试").OnClick(func() { ps.err = "" })
		})
		return
	case info == nil:
		ui.Box(c).Role(ui.RoleStatus).Label("加载中")
		sh := app.shimmer(c)
		sh.bone(c, 4).Height(12).Width(180)
		sh.bone(c, 4).Height(12).Width(320)
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		for i, t := range info.Tags {
			ui.Text(c.Key(i), t).FontSize(11.5).FontWeight(500).TextColor(p.muted).Padding(2, 8).Radius(6).Background(p.surface)
		}
		if info.Followers != "" {
			ui.Text(c, info.Followers+" 粉丝").FontSize(12.5).TextColor(p.muted)
		}
		if info.Likes != "" {
			ui.Text(c, info.Likes+" 获赞").FontSize(12.5).TextColor(p.muted)
		}
	})
	if info.Desc != "" {
		ui.Text(c, info.Desc).FontSize(13).TextColor(p.muted).MaxLines(3).MaxWidth(680)
	}
}
