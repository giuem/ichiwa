package app

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

// The collection page (docs/plugin-design.md §14): every page of a
// source's navigation, an actor's page, a ranking, a filter page. It
// draws what the source returns: an optional header, the filter rows,
// the sections and the cards, page by page.

// colState is a collection page: its filters and its pages.
type colState struct {
	id string
	// title is the navigation item's or the card's, until the page says.
	title string
	// card is the card the page opened from, whose image stands in for
	// the header's until it comes.
	card   *Card
	rows   []FilterRow
	chosen map[string]string
	first  *source.CollectionPage // the first page: title, header, sections, layout
	// haveFirst tells first is the first page of the current choice, one
	// whose list did not fail: a retry's first page replaces one that did.
	haveFirst bool
	// header is first's, or the one the cache kept, shown until it comes.
	header   *source.Header
	results  Feed
	sections map[string]*Feed // sections the host loads, by collection ID
}

func newCol(id, title string) *colState {
	cs := &colState{id: id, title: title, sections: map[string]*Feed{}}
	cs.results.more = true
	return cs
}

// col is the collection page of a navigation item of the source shown.
func (app *App) col(id string) *colState {
	if app.cols == nil {
		app.cols = map[string]*colState{}
	}
	cs := app.cols[id]
	if cs == nil {
		cs = newCol(id, app.navTitle(id))
		cs.results.src = app.src
		if app.cacheDir != "" && app.src != nil {
			cs.results.cache = feedCachePath(app.cacheDir, app.sourceID(), id)
			if c := loadFeedCache(cs.results.cache); c == nil {
				if c = app.legacyFeedCache(id, cs.results.cache); c != nil {
					cs.results.items, cs.header = c.Items, c.Header
				}
			} else {
				cs.results.items, cs.header = c.Items, c.Header
			}
			cs.results.stale = len(cs.results.items) > 0
		}
		app.cols[id] = cs
	}
	return cs
}

// ranked tells the page ranks its cards.
func (cs *colState) ranked() bool { return cs.first != nil && cs.first.Layout == source.LayoutRanked }

func (cs *colState) applyPage(fp feedPage) {
	if fp.page == nil || cs.haveFirst {
		return
	}
	cs.haveFirst = !fp.failed
	cs.first = fp.page
	if fp.page.Header != nil {
		cs.header = fp.page.Header // else the header shown stays, as a retry's page may lack it
	}
	cs.rows = fp.page.Filters
	next := map[string]string{}
	for _, row := range cs.rows {
		if row.Value != "" {
			next[row.Key] = row.Value
		}
	}
	cs.chosen = next
}

// choose picks value in the row key, "" for all, and loads the page for
// the new choice. A ranking drops the rows after the one changed, whose
// options belong to the choice before, until the page brings its own.
func (cs *colState) choose(key, value string) bool {
	if cs.chosen[key] == value {
		return false
	}
	if cs.chosen == nil {
		cs.chosen = map[string]string{}
	}
	if value == "" {
		delete(cs.chosen, key)
	} else {
		cs.chosen[key] = value
	}
	if cs.ranked() {
		if i := slices.IndexFunc(cs.rows, func(r FilterRow) bool { return r.Key == key }); i >= 0 {
			cs.rows = cs.rows[:i+1]
		}
	}
	cs.restart()
	return true
}

// clear goes back to the page without filters.
func (cs *colState) clear() {
	cs.chosen = nil
	cs.restart()
}

// restart drops the results, and loads in flight, for a new choice. The
// view gives the feed a fetch for it.
func (cs *colState) restart() {
	cs.haveFirst = false
	cs.results.reset()
	cs.results.fetch = nil
}

// refresh loads the page anew, its sections too.
func (cs *colState) refresh() {
	cs.restart()
	for _, f := range cs.sections {
		f.reset()
	}
	cs.sections = map[string]*Feed{}
}

// ensure gives the page's feed its fetch, for the current choice, and
// loads its first page when it has none.
func (app *App) ensure(cs *colState) {
	r := &cs.results
	if r.fetch == nil {
		src := r.src
		if src == nil {
			src = app.src
		}
		r.setFetch(src, collectionFetch(source.CollectionRequest{ID: cs.id, Filters: maps.Clone(cs.chosen)}))
		r.onPage = cs.applyPage
		r.more = !r.loaded
	}
	if !r.loaded && !r.loading && r.err == "" {
		app.loadMore(r) // also a load canceled as the page was left
	}
}

// openCollection opens the collection a card or a section names, over the
// page shown.
func (app *App) openCollection(a Card) {
	app.leavePage()
	cs := newCol(a.ID, a.Title)
	card := a
	cs.card = &card
	cs.results.src = app.src
	app.stack = append(app.stack, cs)
}

// closeCollection goes back from the collection opened last.
func (app *App) closeCollection() {
	app.leavePage()
	if n := len(app.stack); n > 0 {
		app.stack[n-1].results.reset()
		for _, f := range app.stack[n-1].sections {
			f.reset()
		}
		app.stack = app.stack[:n-1]
	}
}

// title is what the page is called.
func (cs *colState) pageTitle() string {
	switch {
	case cs.header != nil && cs.header.Title != "":
		return cs.header.Title
	case cs.first != nil && cs.first.Title != "":
		return cs.first.Title
	}
	return cs.title
}

func (app *App) collectionPage(c *ui.Context, cs *colState, opened bool) {
	p := app.pal
	r := &cs.results
	app.ensure(cs)
	retry := func() { r.err = ""; app.loadMore(r) }
	portrait := opened && (cs.header != nil && cs.header.Image != "" || cs.card != nil && cs.card.Image != "")
	if opened {
		c.OnShortcut(0, ui.KeyEscape, app.closeCollection)
		bar := c.TitleBar()
		app.tell(c, cs.pageTitle())
		ui.Row(c).Padding(max(bar.Height, 28)+4, pagePad-8, 6).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
			iconButton(c, iconChevronLeft, 18, p.muted, "返回").OnClick(app.closeCollection)
			if !portrait {
				ui.Text(c, cs.pageTitle()).FontSize(20).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
			}
		})
	} else {
		subtitle := ""
		if cs.header != nil {
			subtitle = cs.header.Description
		}
		app.header(c, cs.pageTitle(), subtitle, func() {
			switch {
			case len(cs.rows) == 0:
				if iconButton(c, iconRefresh, 16, p.muted, "刷新").Clicked() {
					cs.refresh()
				}
			case !cs.ranked() && len(cs.chosen) > 0:
				app.secondary(c, iconRefresh, "重置").OnClick(cs.clear)
			}
		})
	}
	if len(r.items) == 0 && cs.first == nil && !portrait {
		if r.err != "" {
			app.centered(c, iconX, "加载失败", r.errText(), retry)
		} else {
			app.skeletonGrid(c, 3)
		}
		return
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("col-"+cs.id)).TrackScroll(&r.scroll).Grow(1).Padding(4, pagePad, 0).Children(func() {
		if portrait {
			app.portraitHeader(c, cs, retry)
		}
		app.filterRows(c, cs)
		if f := cs.first; f != nil {
			for i, sec := range f.Sections {
				app.section(c.Key(fmt.Sprint("sec", i)), cs, sec, n)
			}
		}
		switch {
		case len(r.items) > 0:
			if cs.first != nil && len(cs.first.Sections) > 0 {
				ui.Text(c, "全部").FontSize(17).Bold().TextColor(p.text).Margin(8, 0, 14).Role(ui.RoleHeading).Level(2)
			} else if portrait {
				ui.Text(c, "作品").FontSize(17).Bold().TextColor(p.text).Margin(30, 0, 16).Role(ui.RoleHeading).Level(2)
			}
			layout := source.LayoutGrid
			if cs.first != nil {
				layout = cs.first.Layout
			}
			app.cards(c, visible(r, n), layout)
			app.feedFooter(c, r.loading, r.errText(), r.more, retry)
		case r.err != "":
			if !portrait || cs.first != nil {
				app.feedFooter(c, false, r.errText(), true, retry)
			}
		case r.loading || !r.loaded:
			app.feedFooter(c, true, "", true, retry)
		case len(cs.rows) > 0 && !cs.ranked():
			ui.Text(c, "没有符合条件的内容，换个条件试试").FontSize(13).TextColor(p.muted).Padding(40, 0).AlignSelf(ui.Center)
		case cs.first == nil || len(cs.first.Sections) == 0:
			ui.Text(c, "这里还没有内容").FontSize(13).TextColor(p.muted).Padding(40, 0).AlignSelf(ui.Center)
		}
	})
	app.pump(c, r)
}

// cards draws the cards of a list as its layout says: a grid of covers,
// a ranking, or a list of rows. People, round, go in rows of their own.
func (app *App) cards(c *ui.Context, items []Card, layout source.Layout) {
	if len(items) == 0 {
		return
	}
	if items[0].Shape == source.ShapeCircle {
		app.actorGrid(c, items, layout == source.LayoutRanked)
		return
	}
	switch layout {
	case source.LayoutRanked:
		app.grid(c, items, app.rankExtras(c))
	case source.LayoutList:
		app.cardList(c, items)
	default:
		app.grid(c, items, episodesExtras)
	}
}

// rankExtras puts a card's place under its title, the first three in
// the accent.
func (app *App) rankExtras(c *ui.Context) func(i int, s Card) cardExtras {
	p := app.pal
	return func(i int, s Card) cardExtras {
		return cardExtras{badge: cardBadge(s), under: func() string {
			ui.Row(c).Gap(6).Children(func() {
				n := ui.Text(c, "TOP "+itoa(i+1)).FontSize(11).Bold()
				if i < 3 {
					n.TextColor(p.accent)
				} else {
					n.TextColor(p.muted)
				}
				if s.Note != "" {
					ui.Text(c, s.Note).FontSize(11).TextColor(p.muted).SingleLine()
				}
			})
			return joinNonEmpty(fmt.Sprintf("第%d名", i+1), s.Note)
		}}
	}
}

// cardList draws cards as rows: a small cover beside the title.
func (app *App) cardList(c *ui.Context, items []Card) {
	p := app.pal
	ui.Column(c).Gap(4).Children(func() {
		for _, s := range items {
			badge := cardBadge(s)
			row := ui.ButtonBase(c.Key("row-" + s.ID)).Gap(12).Padding(6).Radius(10).AlignItems(ui.Start).Label(s.Title).
				Description(joinNonEmpty(badge, s.Subtitle, s.Note))
			pressable(row, ui.Transparent, p.hover, 0.98)
			row.Children(func() {
				app.poster(c, s.Image, s.Title, 7).Width(56).Shrink(0)
				ui.Column(c).Grow(1).Gap(4).Padding(2, 0).Children(func() {
					ui.Text(c, s.Title).FontSize(13.5).FontWeight(600).MaxLines(2).TextColor(p.text)
					if line := joinNonEmpty(badge, s.Note); line != "" {
						ui.Text(c, line).FontSize(11.5).TextColor(p.muted)
					}
					if s.Subtitle != "" {
						ui.Text(c, s.Subtitle).FontSize(11.5).TextColor(p.faint).MaxLines(2)
					}
				})
			})
			if row.Clicked() {
				app.openCard(app.sourceID(), s)
			}
		}
	})
}

// filterRows draws the filter rows. A ranking's first row is its tabs, and
// the rest rows of chips; any other page's rows are a filter panel, under
// which a line sets the cards apart.
func (app *App) filterRows(c *ui.Context, cs *colState) {
	p := app.pal
	if len(cs.rows) == 0 {
		return
	}
	value := func(row FilterRow) string {
		if v, ok := cs.chosen[row.Key]; ok {
			return v
		}
		return row.Value
	}
	if cs.ranked() {
		ui.Column(c).Padding(0, 0, 18).Gap(14).Children(func() {
			row0 := cs.rows[0]
			cur0 := value(row0)
			ui.Row(c).Gap(22).Role(ui.RoleTabList).Label(row0.Title).FocusGroup(ui.Horizontal).Children(func() {
				for _, opt := range row0.Options {
					selected := opt.Value == cur0
					if app.tab(c.Key("tab-"+opt.Value), opt.Title, selected).Clicked() && !selected {
						cs.choose(row0.Key, opt.Value)
					}
				}
			})
			for _, row := range cs.rows[1:] {
				if len(row.Options) == 0 {
					continue
				}
				cur := value(row)
				choices(c.Key("row-"+row.Key), row.Title).Gap(8).Wrap().Children(func() {
					for _, opt := range row.Options {
						selected := opt.Value == cur
						if radio(app.chip(c.Key(opt.Value), opt.Title, selected), selected).Clicked() && !selected {
							cs.choose(row.Key, opt.Value)
						}
					}
				})
			}
		})
		return
	}
	// A row whose "all" says which row it is needs no title beside it.
	titled := slices.ContainsFunc(cs.rows, func(row FilterRow) bool {
		return len(row.Options) == 0 || row.Options[0].Value != "" || row.Options[0].Title == "全部"
	})
	ui.Column(c).Gap(6).Children(func() {
		for _, row := range cs.rows {
			choices(c.Key("row-"+row.Key), row.Title).Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				key := row.Key
				if titled {
					ui.Text(c, row.Title).FontSize(12.5).FontWeight(600).TextColor(p.muted).Width(56).SingleLine()
				}
				cur := value(row)
				for _, o := range row.Options {
					value := o.Value
					optKey := value
					if optKey == "" {
						optKey = "all"
					}
					el := app.option(c.Key(optKey), o.Title, cur == value)
					if value == "" && !strings.HasPrefix(o.Title, "全部") {
						el.Label("全部" + o.Title)
					}
					el.OnClick(func() { cs.choose(key, value) })
				}
			})
		}
	})
	ui.Box(c).Height(1).Background(p.hairline).Margin(14, 0, 22)
}

// portraitHeader is the header of a page about someone or something with
// a picture: the picture, the name, and what the header says.
func (app *App) portraitHeader(c *ui.Context, cs *colState, retry func()) {
	p := app.pal
	name := cs.pageTitle()
	image, shape := "", source.ShapeCircle
	if cs.card != nil {
		image = cs.card.Image
		if cs.card.Shape != "" {
			shape = cs.card.Shape
		}
	}
	if h := cs.header; h != nil {
		if h.Image != "" {
			image = h.Image
		}
		if h.Shape != "" {
			shape = h.Shape
		}
	}
	ui.Row(c).Gap(22).AlignItems(ui.Center).Children(func() {
		if shape == source.ShapeCircle {
			app.avatar(c, image, name, 96)
		} else {
			app.poster(c, image, name, 10).Width(96).Shrink(0)
		}
		ui.Column(c).Grow(1).Shrink(1).Gap(8).Children(func() {
			ui.Text(c, name).FontSize(26).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
			r := &cs.results
			switch info := cs.header; {
			case info == nil && cs.first == nil && r.err != "":
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, r.errText()).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
					app.secondary(c, iconRefresh, "重试").OnClick(retry)
				})
			case info == nil && cs.first == nil:
				ui.Box(c).Role(ui.RoleStatus).Label("加载中")
				sh := app.shimmer(c)
				sh.bone(c, 4).Height(12).Width(180)
				sh.bone(c, 4).Height(12).Width(320)
			case info != nil:
				app.headerFacts(c, info)
			}
		})
	})
}

// headerFacts shows what a header says under its title.
func (app *App) headerFacts(c *ui.Context, info *source.Header) {
	p := app.pal
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

// sectionFeed is the feed of a section that names its collection rather
// than give its cards. A section is one page: loaded when shown, again
// when a load was canceled as the page was left.
func (app *App) sectionFeed(feeds map[string]*Feed, src source.Source, id string) *Feed {
	f := feeds[id]
	if f == nil {
		f = newFeed(src, collectionFetch(source.CollectionRequest{ID: id}))
		feeds[id] = f
	}
	if !f.loaded && !f.loading && f.err == "" {
		f.more = true
		app.loadMore(f)
	}
	f.more = false
	return f
}

// section draws a section of a collection page: its cards, given or
// loaded from its collection, in one row, a grid or a ranking.
func (app *App) section(c *ui.Context, cs *colState, sec Section, n int) {
	p := app.pal
	items := sec.Items
	var f *Feed
	if len(items) == 0 && sec.Collection != "" {
		f = app.sectionFeed(cs.sections, cs.results.src, sec.Collection)
		items = f.items
	}
	ui.Column(c).Margin(0, 0, 18).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 12).Children(func() {
			ui.Text(c, sec.Title).FontSize(17).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			if sec.More != "" {
				more := sec
				app.secondary(c, nil, "查看全部").OnClick(func() {
					app.openCollection(Card{Kind: source.CardCollection, ID: more.More, Title: more.Title})
				})
			}
		})
		switch {
		case len(items) > 0:
			shown := items
			if sec.Layout != source.SectionGrid && len(shown) > n {
				shown = shown[:n]
			}
			layout := source.LayoutGrid
			if sec.Layout == source.SectionRanked {
				layout = source.LayoutRanked
			}
			app.cards(c, shown, layout)
		case f != nil && f.err != "":
			app.feedFooter(c, false, f.errText(), true, func() { f.err = ""; f.more = true; app.loadMore(f) })
		case f != nil && f.loading:
			app.feedFooter(c, true, "", true, nil)
		}
	})
}
