package app

import (
	"fmt"
	"maps"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

// The collection page of a plugin (docs/plugin-design.md §14): an
// optional header, the filter rows, the sections and the cards of a
// collection, from what the plugin returns. It is plain for now; a later
// step draws it properly, with Hongguo moved to a plugin.

// colState is a collection page: its filters and its pages.
type colState struct {
	id     string
	rows   []FilterRow
	chosen map[string]string
	first  *source.CollectionPage // the first page: title, header, sections, layout
	// haveFirst tells first is the first page of the current choice, one
	// whose list did not fail: a retry's first page replaces one that did.
	haveFirst bool
	results   Feed
	sections  map[string]*Feed // sections the host loads, by collection ID
}

func (app *App) col(id string) *colState {
	if app.cols == nil {
		app.cols = map[string]*colState{}
	}
	cs := app.cols[id]
	if cs == nil {
		cs = &colState{id: id, sections: map[string]*Feed{}}
		cs.results.more = true
		app.cols[id] = cs
	}
	return cs
}

func (cs *colState) applyPage(fp feedPage) {
	if fp.page == nil || cs.haveFirst {
		return
	}
	cs.haveFirst = !fp.failed
	cs.first = fp.page
	cs.rows = fp.page.Filters
	next := map[string]string{}
	for _, row := range cs.rows {
		if row.Value != "" {
			next[row.Key] = row.Value
		}
	}
	cs.chosen = next
}

func (cs *colState) choose(key, value string) {
	if cs.chosen[key] == value {
		return
	}
	if cs.chosen == nil {
		cs.chosen = map[string]string{}
	}
	if value == "" {
		delete(cs.chosen, key)
	} else {
		cs.chosen[key] = value
	}
	cs.restart()
}

func (cs *colState) restart() {
	cs.haveFirst = false
	cs.results.reset()
	cs.results.fetch = nil
}

func (app *App) collectionPage(c *ui.Context, id string) {
	cs := app.col(id)
	p := app.pal
	r := &cs.results
	if r.fetch == nil {
		r.setFetch(app.src, collectionFetch(source.CollectionRequest{ID: id, Filters: maps.Clone(cs.chosen)}))
		r.onPage = cs.applyPage
		r.more = !r.loaded
		r.exact = true
	}
	if !r.loaded && !r.loading && r.err == "" {
		app.loadMore(r)
	}
	retry := func() { r.err = ""; app.loadMore(r) }
	title := app.navTitle(id)
	subtitle := ""
	layout := source.LayoutGrid
	if f := cs.first; f != nil {
		if f.Title != "" {
			title = f.Title
		}
		if f.Header != nil {
			title = f.Header.Title
			subtitle = f.Header.Description
		}
		layout = f.Layout
	}
	app.header(c, title, subtitle, func() {
		if iconButton(c, iconRefresh, 16, p.muted, "刷新").Clicked() {
			cs.restart()
			cs.sections = map[string]*Feed{}
		}
	})
	if !r.loaded && len(r.items) == 0 && cs.first == nil {
		if r.err != "" {
			app.centered(c, iconX, "加载失败", r.err, retry)
		} else {
			app.skeletonGrid(c, 3)
		}
		return
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("col-"+id)).TrackScroll(&r.scroll).Grow(1).Padding(0, pagePad, 0).Children(func() {
		if f := cs.first; f != nil && f.Header != nil && len(f.Header.Meta) > 0 {
			ui.Row(c).Gap(8).Wrap().Margin(0, 0, 16).Children(func() {
				for i, m := range f.Header.Meta {
					ui.Text(c.Key(i), joinNonEmpty(m.Value, m.Label)).FontSize(12).TextColor(p.muted).Padding(2, 8).Radius(6).Background(p.surface)
				}
			})
		}
		if len(cs.rows) > 0 {
			ui.Column(c).Gap(6).Margin(0, 0, 18).Children(func() {
				for _, row := range cs.rows {
					choices(c.Key("row-"+row.Key), row.Title).Gap(4).Wrap().Children(func() {
						key := row.Key
						ui.Text(c, row.Title).FontSize(12.5).FontWeight(600).TextColor(p.muted).Width(56)
						for _, o := range row.Options {
							value := o.Value
							k := value
							if k == "" {
								k = "all"
							}
							app.option(c.Key(k), o.Title, cs.chosen[key] == value).OnClick(func() { cs.choose(key, value) })
						}
					})
				}
			})
		}
		if f := cs.first; f != nil {
			for i, sec := range f.Sections {
				app.section(c.Key(fmt.Sprint("sec", i)), cs, sec, n)
			}
		}
		ranked := layout == source.LayoutRanked
		switch {
		case len(r.items) > 0:
			if cs.first != nil && len(cs.first.Sections) > 0 {
				ui.Text(c, "全部").FontSize(17).Bold().TextColor(p.text).Margin(8, 0, 14).Role(ui.RoleHeading).Level(2)
			}
			app.grid(c, visible(r, n), func(i int, s Card) cardExtras {
				if ranked {
					return cardExtras{badge: fmt.Sprintf("第 %d 名", i+1)}
				}
				return episodesExtras(i, s)
			})
			app.feedFooter(c, r.loading, r.err, r.more, retry)
		case r.err != "":
			app.feedFooter(c, false, r.err, true, retry)
		case r.loading:
			app.feedFooter(c, true, "", true, retry)
		case cs.first == nil || len(cs.first.Sections) == 0:
			ui.Text(c, "这里还没有内容").FontSize(13).TextColor(p.muted).Padding(40, 0).AlignSelf(ui.Center)
		}
	})
	app.pump(c, r)
}

// section draws a section of a collection page: its cards, given or
// loaded from its collection, in one row.
func (app *App) section(c *ui.Context, cs *colState, sec Section, n int) {
	p := app.pal
	items := sec.Items
	var f *Feed
	if len(items) == 0 && sec.Collection != "" {
		f = cs.sections[sec.Collection]
		if f == nil {
			f = newFeed(app.src, collectionFetch(source.CollectionRequest{ID: sec.Collection}))
			cs.sections[sec.Collection] = f
		}
		// A section is one page: loaded when shown, again when a load
		// was canceled as the page was left.
		if !f.loaded && !f.loading && f.err == "" {
			f.more = true
			app.loadMore(f)
		}
		f.more = false
		items = f.items
	}
	ui.Column(c).Margin(0, 0, 18).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 12).Children(func() {
			ui.Text(c, sec.Title).FontSize(17).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			if sec.More != "" {
				more := sec
				app.secondary(c, nil, "查看全部").OnClick(func() {
					app.openPerson(Card{Kind: source.CardCollection, ID: more.More, Title: more.Title})
				})
			}
		})
		switch {
		case len(items) > 0:
			shown := items
			if sec.Layout != source.SectionGrid && len(shown) > n {
				shown = shown[:n]
			}
			ranked := sec.Layout == source.SectionRanked
			app.grid(c, shown, func(i int, s Card) cardExtras {
				if ranked {
					return cardExtras{badge: fmt.Sprintf("第 %d 名", i+1)}
				}
				return episodesExtras(i, s)
			})
		case f != nil && f.err != "":
			app.feedFooter(c, false, f.err, true, func() { f.err = ""; f.more = true; app.loadMore(f) })
		case f != nil && f.loading:
			app.feedFooter(c, true, "", true, nil)
		}
	})
}
