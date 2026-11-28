package ui

import (
	"fmt"
	"slices"
	"strings"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

// The collection page (docs/plugin-design.md §14): every page of a
// source's navigation, an actor's page, a ranking, a filter page. It
// draws what the source returns: an optional header, the filter rows,
// the sections and the cards, page by page.

func (app *App) collectionPage(c *ui.Context, cs *Collection, opened bool) {
	p := app.pal
	r := &cs.Results
	app.Ensure(cs)
	retry := func() { r.Err = ""; app.LoadMore(r) }
	portrait := opened && (cs.Header != nil && cs.Header.Image != "" || cs.Card != nil && cs.Card.Image != "")
	if opened {
		c.OnShortcut(0, ui.KeyEscape, app.CloseCollection)
		bar := c.TitleBar()
		app.tell(c, cs.PageTitle())
		ui.Row(c).Padding(max(bar.Height, 28)+4, pagePad-8, 6).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
			iconButton(c, iconChevronLeft, 18, p.muted, "返回").OnClick(app.CloseCollection)
			if !portrait {
				ui.Text(c, cs.PageTitle()).FontSize(20).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
			}
		})
	} else {
		subtitle := ""
		if cs.Header != nil {
			subtitle = cs.Header.Description
		}
		app.header(c, cs.PageTitle(), subtitle, func() {
			switch {
			case len(cs.Rows) == 0:
				if iconButton(c, iconRefresh, 16, p.muted, "刷新").Clicked() {
					cs.Refresh()
				}
			case !cs.Ranked() && len(cs.Chosen) > 0:
				app.secondary(c, iconRefresh, "重置").OnClick(cs.Clear)
			}
		})
	}
	if len(r.Items) == 0 && cs.First == nil && !portrait {
		if r.Err != "" {
			app.centered(c, iconX, "加载失败", r.ErrText(), retry)
		} else {
			app.skeletonGrid(c, 3)
		}
		return
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("col-"+cs.ID)).TrackScroll(scrollOf(r)).Grow(1).Padding(4, pagePad, 0).Children(func() {
		if portrait {
			app.portraitHeader(c, cs, retry)
		}
		app.filterRows(c, cs)
		if f := cs.First; f != nil {
			for i, sec := range f.Sections {
				app.section(c.Key(fmt.Sprint("sec", i)), cs, sec, n)
			}
		}
		switch {
		case len(r.Items) > 0:
			if cs.First != nil && len(cs.First.Sections) > 0 {
				ui.Text(c, "全部").FontSize(17).Bold().TextColor(p.text).Margin(8, 0, 14).Role(ui.RoleHeading).Level(2)
			} else if portrait {
				ui.Text(c, "作品").FontSize(17).Bold().TextColor(p.text).Margin(30, 0, 16).Role(ui.RoleHeading).Level(2)
			}
			layout := source.LayoutGrid
			if cs.First != nil {
				layout = cs.First.Layout
			}
			app.cards(c, core.Visible(r, n), layout)
			app.feedFooter(c, r.Loading, r.ErrText(), r.More, retry)
		case r.Err != "":
			if !portrait || cs.First != nil {
				app.feedFooter(c, false, r.ErrText(), true, retry)
			}
		case r.Loading || !r.Loaded:
			app.feedFooter(c, true, "", true, retry)
		case len(cs.Rows) > 0 && !cs.Ranked():
			ui.Text(c, "没有符合条件的内容，换个条件试试").FontSize(13).TextColor(p.muted).Padding(40, 0).AlignSelf(ui.Center)
		case cs.First == nil || len(cs.First.Sections) == 0:
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
				app.openCard(app.SourceID(), s)
			}
		}
	})
}

// filterRows draws the filter rows. A ranking's first row is its tabs, and
// the rest rows of chips; any other page's rows are a filter panel, under
// which a line sets the cards apart.
func (app *App) filterRows(c *ui.Context, cs *Collection) {
	p := app.pal
	if len(cs.Rows) == 0 {
		return
	}
	value := func(row FilterRow) string {
		if v, ok := cs.Chosen[row.Key]; ok {
			return v
		}
		return row.Value
	}
	if cs.Ranked() {
		ui.Column(c).Padding(0, 0, 18).Gap(14).Children(func() {
			row0 := cs.Rows[0]
			cur0 := value(row0)
			ui.Row(c).Gap(22).Role(ui.RoleTabList).Label(row0.Title).FocusGroup(ui.Horizontal).Children(func() {
				for _, opt := range row0.Options {
					selected := opt.Value == cur0
					if app.tab(c.Key("tab-"+opt.Value), opt.Title, selected).Clicked() && !selected {
						cs.Choose(row0.Key, opt.Value)
					}
				}
			})
			for _, row := range cs.Rows[1:] {
				if len(row.Options) == 0 {
					continue
				}
				cur := value(row)
				choices(c.Key("row-"+row.Key), row.Title).Gap(8).Wrap().Children(func() {
					for _, opt := range row.Options {
						selected := opt.Value == cur
						if radio(app.chip(c.Key(opt.Value), opt.Title, selected), selected).Clicked() && !selected {
							cs.Choose(row.Key, opt.Value)
						}
					}
				})
			}
		})
		return
	}
	// A row whose "all" says which row it is needs no title beside it.
	titled := slices.ContainsFunc(cs.Rows, func(row FilterRow) bool {
		return len(row.Options) == 0 || row.Options[0].Value != "" || row.Options[0].Title == "全部"
	})
	ui.Column(c).Gap(6).Children(func() {
		for _, row := range cs.Rows {
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
					el.OnClick(func() { cs.Choose(key, value) })
				}
			})
		}
	})
	ui.Box(c).Height(1).Background(p.hairline).Margin(14, 0, 22)
}

// portraitHeader is the header of a page about someone or something with
// a picture: the picture, the name, and what the header says.
func (app *App) portraitHeader(c *ui.Context, cs *Collection, retry func()) {
	p := app.pal
	name := cs.PageTitle()
	image, shape := "", source.ShapeCircle
	if cs.Card != nil {
		image = cs.Card.Image
		if cs.Card.Shape != "" {
			shape = cs.Card.Shape
		}
	}
	if h := cs.Header; h != nil {
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
			r := &cs.Results
			switch info := cs.Header; {
			case info == nil && cs.First == nil && r.Err != "":
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, r.ErrText()).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
					app.secondary(c, iconRefresh, "重试").OnClick(retry)
				})
			case info == nil && cs.First == nil:
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

// section draws a section of a collection page: its cards, given or
// loaded from its collection, in one row, a grid or a ranking.
func (app *App) section(c *ui.Context, cs *Collection, sec Section, n int) {
	p := app.pal
	items := sec.Items
	var f *Feed
	if len(items) == 0 && sec.Collection != "" {
		f = app.ColSection(cs, sec.Collection)
		items = f.Items
	}
	ui.Column(c).Margin(0, 0, 18).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 12).Children(func() {
			ui.Text(c, sec.Title).FontSize(17).Bold().TextColor(p.text).Grow(1).Role(ui.RoleHeading).Level(2)
			if sec.More != "" {
				more := sec
				app.secondary(c, nil, "查看全部").OnClick(func() {
					app.OpenCollection(Card{Kind: source.CardCollection, ID: more.More, Title: more.Title})
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
		case f != nil && f.Err != "":
			app.feedFooter(c, false, f.ErrText(), true, func() { f.Err = ""; f.More = true; app.LoadMore(f) })
		case f != nil && f.Loading:
			app.feedFooter(c, true, "", true, nil)
		}
	})
}
