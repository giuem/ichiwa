package app

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
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

// ---- Filter ----

type filterState struct {
	rows    []FilterRow
	rowsOK  bool
	chosen  map[string]string // a value per row key, none for all
	results Feed              // for chosen; without a fetch until shown
}

// choose picks value in the row key, "" for all, and reports whether the
// choice changed. Dependent row resets are returned by the source in the
// updated FilterRows on the first page.
func (fs *filterState) choose(key, value string) bool {
	if fs.chosen[key] == value {
		return false
	}
	if fs.chosen == nil {
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

func (fs *filterState) applyPage(fp feedPage) {
	if fp.page == nil || len(fp.page.Filters) == 0 {
		return
	}
	fs.rows = fp.page.Filters
	fs.rowsOK = true
	next := map[string]string{}
	for _, row := range fs.rows {
		if row.Value != "" {
			next[row.Key] = row.Value
		}
	}
	fs.chosen = next
}

func (app *App) filterPage(c *ui.Context) {
	fs := &app.filter
	p := app.pal
	r := &fs.results
	if r.fetch == nil {
		chosen := maps.Clone(fs.chosen) // the fetch runs off the main thread
		r.fetch = func(cursor string) (feedPage, error) {
			return pageResult(app.src.Collection(context.Background(), source.CollectionRequest{
				ID:      pageFilter,
				Filters: chosen,
				Cursor:  cursor,
			}))
		}
		r.onPage = fs.applyPage
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
		case !fs.rowsOK && r.err != "":
			// Filter panel already shows the error and retry button.
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
		if fs.results.err != "" {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(4, 0).Children(func() {
				ui.Text(c, "筛选条件加载失败："+fs.results.err).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
				app.secondary(c, iconRefresh, "重试").OnClick(func() { fs.results.err = ""; app.loadMore(&fs.results) })
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
			choices(c.Key("row-"+row.Key), row.Title).Gap(4).Wrap().Children(func() {
				key := row.Key
				for _, o := range row.Options {
					value := o.Value
					optKey := value
					if optKey == "" {
						optKey = "all"
					}
					el := app.option(c.Key(optKey), o.Title, fs.chosen[key] == value)
					if value == "" && !strings.HasPrefix(o.Title, "全部") {
						el.Label("全部" + o.Title)
					}
					el.OnClick(func() { fs.choose(key, value) })
				}
			})
		}
	})
}

// ---- Actors ----

const actorCardMinW = 300

// actorGrid lays out the rows of the actors' ranking.
func (app *App) actorGrid(c *ui.Context, actors []Card) {
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

func (app *App) actorCard(c *ui.Context, i int, a Card, width float32) {
	p := app.pal
	b := ui.ButtonBase(c.Key(a.ID)).Width(width).Padding(14, 16).Gap(14).Radius(14).
		AlignItems(ui.Center).Justify(ui.Start).Label(a.Title).
		Description(joinNonEmpty(fmt.Sprintf("第%d名", i+1), a.Note, a.Subtitle))
	pressable(b, p.surface, p.surface.Mix(p.text, 0.05), 0.98)
	b.Children(func() {
		ui.Text(c, itoa(i+1)).FontSize(15).Bold().Width(22).TextAlign(ui.Center).TextColor(ternary(i < 3, p.accent, p.faint))
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
	b.OnClick(func() { app.openPerson(a) })
}

// ---- An actor's page ----

// personState is an actor's collection page, shown over the sidebar's page.
type personState struct {
	actor        Card // what the ranking knew, shown until the header loads
	header       *source.Header
	headerBusy   bool
	headerLoaded bool
	headerErr    string
	works        Feed
	worksReady   bool
}

func (app *App) openPerson(a Card) {
	app.person = &personState{actor: a}
}

func (app *App) loadPerson(ps *personState) {
	if app.src == nil || ps.headerBusy {
		return
	}
	ps.headerBusy, ps.headerErr = true, ""
	src := app.src
	colID := ps.actor.ID
	async(app, func() (*source.CollectionPage, error) {
		return src.Collection(context.Background(), source.CollectionRequest{ID: colID})
	}, func(p *source.CollectionPage, err error) {
		if app.person != ps {
			return
		}
		ps.headerBusy = false
		if err != nil {
			ps.headerErr = errText(err)
			return
		}
		ps.headerLoaded = true
		if p == nil {
			ps.worksReady = true
			ps.works.loaded = true
			return
		}
		if p.Header != nil {
			ps.header = p.Header
		}
		// If the source splits works into a lazy section (e.g. Hongguo's
		// person-works:<id>), load and paginate it independently so the bio
		// never waits for works and both can fail/retry separately.
		if len(p.Items) == 0 && p.Cursor == "" {
			for _, sec := range p.Sections {
				if sec.Collection != "" {
					worksCol := sec.Collection
					ps.works = *newFeed(func(cursor string) (feedPage, error) {
						return pageResult(src.Collection(context.Background(), source.CollectionRequest{
							ID:     worksCol,
							Cursor: cursor,
						}))
					})
					ps.worksReady = true
					app.loadMore(&ps.works)
					return
				}
			}
		}
		// Otherwise the source returned works inline on the first page.
		ps.works = *newFeed(func(cursor string) (feedPage, error) {
			return pageResult(src.Collection(context.Background(), source.CollectionRequest{
				ID:     colID,
				Cursor: cursor,
			}))
		})
		ps.worksReady = true
		ps.works.loaded = true
		fp := feedPage{items: p.Items, cursor: p.Cursor, more: p.Cursor != "", page: p}
		fresh, added := ps.works.take(fp)
		ps.works.more = ps.works.step(fp, fresh)
		app.fill(&ps.works, added)
	})
}

func (app *App) personPage(c *ui.Context) {
	ps := app.person
	p := app.pal
	if !ps.headerLoaded && !ps.headerBusy && ps.headerErr == "" && ps.header == nil {
		app.loadPerson(ps)
	}
	f := &ps.works
	retry := func() { f.err = ""; app.loadMore(f) }
	back := func() { app.person = nil }
	c.OnShortcut(0, ui.KeyEscape, back)

	name, avatar := ps.actor.Title, ps.actor.Image
	if ps.header != nil {
		if ps.header.Title != "" {
			name = ps.header.Title
		}
		if ps.header.Image != "" {
			avatar = ps.header.Image
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
		case ps.headerErr != "" && !ps.worksReady:
			// Header retry is shown in personFacts right above.
		default:
			app.feedFooter(c, f.err == "", f.err, true, retry)
		}
	})
	app.pump(c, f)
}

// personFacts shows what the collection header says under the name.
func (app *App) personFacts(c *ui.Context, ps *personState) {
	p := app.pal
	info := ps.header
	switch {
	case info == nil && ps.headerErr != "":
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ps.headerErr).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
			app.secondary(c, iconRefresh, "重试").OnClick(func() { app.loadPerson(ps) })
		})
		return
	case info == nil && !ps.headerLoaded:
		ui.Box(c).Role(ui.RoleStatus).Label("加载中")
		sh := app.shimmer(c)
		sh.bone(c, 4).Height(12).Width(180)
		sh.bone(c, 4).Height(12).Width(320)
		return
	case info == nil:
		return
	}
	if len(info.Meta) > 0 {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			for i, m := range info.Meta {
				if m.Label == "" {
					ui.Text(c.Key(i), m.Value).FontSize(11.5).FontWeight(500).TextColor(p.muted).Padding(2, 8).Radius(6).Background(p.surface)
				} else {
					ui.Text(c.Key(i), m.Value+" "+m.Label).FontSize(12.5).TextColor(p.muted)
				}
			}
		})
	}
	if info.Description != "" {
		ui.Text(c, info.Description).FontSize(13).TextColor(p.muted).MaxLines(3).MaxWidth(680)
	}
}
