package ui

import (
	"fmt"
	"strings"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

const (
	pagePad  = 32
	cardMinW = 156
	cardGap  = 20
	// titleLine is the line height of a card's title. Titles always take
	// two lines of room so cards in a row line up whatever their length.
	titleLine = 18
)

// header draws a page's title row, which also drags the window.
func (app *App) header(c *ui.Context, title, subtitle string, trailing func()) {
	p := app.pal
	bar := c.TitleBar()
	app.tell(c, title)
	ui.Row(c).Padding(max(bar.Height, 28)+10, pagePad, 18, pagePad).Gap(12).AlignItems(ui.End).DragWindow().Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(28).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(13).TextColor(p.muted)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}

// tell has screen readers read a page's title as the page changes, which
// moves no keyboard focus that would bring it.
func (app *App) tell(c *ui.Context, title string) {
	if title != app.Told {
		app.Told = title
		c.Announce(title)
	}
}

// centered shows a message in the middle of a page, with a button that
// tries again when retry is set: a failure, read out as it appears.
func (app *App) centered(c *ui.Context, icon *ui.SVG, title, detail string, retry func()) {
	p := app.pal
	ui.Column(c).Grow(1).Center().Gap(10).Padding(40).Children(func() {
		ui.Box(c).Size(64, 64).Radius(999).Background(p.surface).Center().Margin(0, 0, 6).Children(func() {
			ui.Icon(c, icon).Size(26, 26).TextColor(p.muted)
		})
		message := ui.Column(c).Gap(10).AlignItems(ui.Center)
		if retry != nil {
			message.Role(ui.RoleAlert)
		}
		message.Children(func() {
			ui.Text(c, title).FontSize(16).Bold().TextColor(p.text)
			if detail != "" {
				ui.Text(c, detail).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
			}
		})
		if retry != nil {
			app.primary(c, iconRefresh, "重试").Margin(10, 0, 0).OnClick(retry)
		}
	})
}

// columns fits cards of at least cardMinW in width w.
func columns(w float32) (int, float32) {
	n := max(2, int((w+cardGap)/(cardMinW+cardGap)))
	return n, (w - cardGap*float32(n-1)) / float32(n)
}

func (app *App) contentWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w - sidebarWidth - 2*pagePad - 12 // and the scroll bar
}

// poster draws a cover, fading it in over a placeholder as it loads. It is
// always beside the title, so assistive technology skips it.
func (app *App) poster(c *ui.Context, url, title string, radius float32) ui.Element {
	p := app.pal
	box := ui.Box(c).AspectRatio(3.0 / 4).Radius(radius).Clip()
	bmp := app.posters.Get(url)
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(p.surface)
	}
	box.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Opacity(1 - in).AccessHidden(true).Children(func() {
				ui.Text(c, firstRune(title)).FontSize(26).Bold().TextColor(p.faint)
			})
		}
		if bmp != nil {
			img := ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover)
			if in < 1 {
				img.Opacity(in)
			}
		}
	})
	return box
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// cardExtras is what a page adds to a drama's card.
type cardExtras struct {
	badge string // over the cover
	// under draws below the title, in place of the note, and returns what
	// screen readers read of it.
	under func() string
	// corner draws controls of their own at the card's bottom right, which
	// a card, being one button, could not hold inside it.
	corner func()
	// onClick overrides the default click action (e.g. for cross-source records).
	onClick func()
}

func cardBadge(s Card) string {
	if len(s.Badges) > 0 {
		return s.Badges[0]
	}
	return ""
}

func episodesExtras(_ int, s Card) cardExtras { return cardExtras{badge: cardBadge(s)} }

func (app *App) openCard(sourceID string, s Card) { app.openFrom(sourceID, s) }

// openFrom opens a card of the source sourceID: a drama's player page, or
// a collection over the pages of its source.
func (app *App) openFrom(sourceID string, s Card) {
	if s.Kind == source.CardCollection {
		app.OpenCollectionOf(sourceID, s)
		return
	}
	app.Open(sourceID, s, -1)
}

// card is a media card in a grid: its cover and title, and the page's extras. A
// click opens it.
func (app *App) card(c *ui.Context, s Card, width float32, x cardExtras) {
	p := app.pal
	ui.Box(c.Key(s.ID)).Width(width).Children(func() {
		b := ui.ButtonBase(c).Width(width).AlignItems(ui.Start).Cursor(ui.CursorPointer).Label(s.Title)
		hover := b.Hovered() || b.FocusVisible()
		lift := b.AnimateTo("lift", boolf(hover), ui.Smooth)
		b.AnimatePose(ui.Pose{Scale: 1 - 0.02*boolf(b.Pressed())}, ui.Snappy)
		note := s.Note
		b.Children(func() {
			ui.Column(c).Width(width).Gap(9).Children(func() {
				poster := app.poster(c, s.Image, s.Title, 10)
				poster.Shadow(0, 2+8*lift, 6+18*lift, 0, p.shade.Alpha(0.10+0.16*lift)).Translate(0, -3*lift)
				poster.Children(func() {
					if x.badge != "" {
						ui.Text(c, x.badge).FontSize(11).FontWeight(600).TextColor(white).
							Padding(3, 7).Radius(6).Background(ui.RGBA(0, 0, 0, 0.5)).
							Absolute().Left(8).Bottom(8)
					}
					// A play button that rises in under the pointer.
					if lift > 0.01 {
						ui.Box(c).Size(40, 40).Radius(999).Background(p.accent).Center().
							Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.3)).
							Absolute().Right(10).Bottom(10).Opacity(lift).Scale(0.7+0.3*lift, 0.7+0.3*lift).
							Children(func() { ui.Icon(c, iconPlayFill).Size(16, 16).TextColor(white).Margin(0, 0, 0, 2) })
					}
				})
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, s.Title).FontSize(13.5).FontWeight(600).
						FixedLineHeight(titleLine).MaxLines(2).MinHeight(2 * titleLine).
						TextColor(b.AnimateColor("title", ternary(hover, p.accent, p.text), quick))
					if x.under != nil {
						note = x.under()
					} else if s.Note != "" {
						ui.Text(c, s.Note).FontSize(11.5).TextColor(p.muted).SingleLine()
					}
				})
			})
		})
		b.Description(joinNonEmpty(x.badge, note))
		if b.Clicked() {
			if x.onClick != nil {
				x.onClick()
			} else {
				app.openCard(app.SourceID(), s)
			}
		}
		if x.corner != nil {
			ui.Box(c).Absolute().Right(0).Bottom(0).Children(x.corner)
		}
	})
}

// grid lays cards out in rows that fill the width.
func (app *App) grid(c *ui.Context, cards []Card, extras func(i int, s Card) cardExtras) {
	n, w := columns(app.contentWidth(c))
	ui.Column(c).Gap(28).Children(func() {
		for start := 0; start < len(cards); start += n {
			ui.Row(c.Key(start)).Gap(cardGap).AlignItems(ui.Start).Children(func() {
				for i := start; i < min(start+n, len(cards)); i++ {
					app.card(c, cards[i], w, extras(i, cards[i]))
				}
			})
		}
	})
}

// skeletonGrid stands in for a grid while it loads.
func (app *App) skeletonGrid(c *ui.Context, rows int) {
	s := app.shimmer(c)
	n, w := columns(app.contentWidth(c))
	ui.Column(c).Grow(1).Padding(4, pagePad, 0).Gap(28).ClipY().Role(ui.RoleStatus).Label("加载中").Children(func() {
		for r := range rows {
			ui.Row(c.Key(r)).Gap(cardGap).Children(func() {
				for i := range n {
					ui.Column(c.Key(i)).Width(w).Gap(9).Children(func() {
						s.bone(c, 10).AspectRatio(3.0 / 4)
						// Two title lines, as tall as a card's title.
						ui.Column(c).Height(2*titleLine).Gap(5).Padding(2, 0).Children(func() {
							s.bone(c, 4).Height(13).WidthPercent(92)
							s.bone(c, 4).Height(13).WidthPercent(float32(40 + (r*n+i)*17%35))
						})
						s.bone(c, 4).Height(10).WidthPercent(36).Margin(5, 0, 0)
					})
				}
			})
		}
	})
}

// feedFooter shows the state of a feed below its grid.
func (app *App) feedFooter(c *ui.Context, loading bool, err string, more bool, retry func()) {
	p := app.pal
	ui.Row(c).Height(84).Center().Gap(10).Children(func() {
		switch {
		case loading:
			ui.Spinner(c)
			ui.Text(c, "加载中").FontSize(12.5).TextColor(p.muted)
		case err != "":
			ui.Text(c, err).FontSize(12.5).TextColor(p.muted).Role(ui.RoleAlert)
			app.secondary(c, iconRefresh, "重试").OnClick(retry)
		case !more:
			ui.Box(c).Width(28).Height(1).Background(p.hairline)
			ui.Text(c, "已经到底了").FontSize(12).TextColor(p.faint)
			ui.Box(c).Width(28).Height(1).Background(p.hairline)
		}
	})
}

// ---- Search ----

func (app *App) searchPage(c *ui.Context) {
	s := &app.Search
	p := app.pal
	app.ShowSearch()
	app.header(c, "搜索", "", nil)
	ui.Column(c).Padding(0, pagePad, 22).Role(ui.RoleSearch).Children(func() {
		f := ui.SearchField(c.Key("search"), &s.Query).Label("搜索").Placeholder("搜索剧名、演员、题材，回车搜索").Bind(&app.searchField).
			MaxWidth(560).Height(40).Radius(10).FontSize(14)
		if s.Focus {
			app.searchField.Focus(c)
			s.Focus = false
		}
		if f.Submitted() {
			app.RunSearch(s.Query)
		}
		if f.Changed() {
			app.Typed(s.Query)
		}
		if len(s.Suggests) > 0 && s.SuggestFor == strings.TrimSpace(s.Query) {
			ui.Column(c.Key("suggests")).MaxWidth(560).Margin(6, 0, 0).Padding(4).Radius(10).Background(p.surface).
				Role(ui.RoleList).Label("搜索建议").Children(func() {
				for i, word := range s.Suggests {
					b := ui.ButtonBase(c.Key(i)).Height(32).PaddingX(10).Radius(7).Justify(ui.Start).AlignItems(ui.Center).Gap(10).Label(word)
					pressable(b, ui.Transparent, p.hover, 0.98)
					b.Children(func() {
						ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.faint)
						ui.Text(c, word).FontSize(13.5).TextColor(p.text).SingleLine()
					})
					if b.Clicked() {
						s.Query = word
						app.RunSearch(word)
					}
				}
			})
		}
	})
	if s.Shown == "" {
		app.hotBoards(c)
		return
	}
	r := &s.Results
	retry := func() { r.Err = ""; app.LoadMore(r) }
	// The focus stays in the field, so nothing else says the search is done.
	if r.Loaded && !r.Loading && r.Err == "" && s.Told != s.Shown {
		s.Told = s.Shown
		c.Announce(ternary(len(r.Items) > 0, "“"+s.Shown+"”的搜索结果已显示", "没有找到“"+s.Shown+"”"))
	}
	if len(r.Items) == 0 {
		switch {
		case r.Loading || !r.Loaded && r.Err == "":
			app.skeletonGrid(c, 2)
		case r.Err != "":
			app.centered(c, iconX, "搜索失败", r.ErrText(), retry)
		default:
			app.centered(c, iconSearch, "没有找到“"+s.Shown+"”", "换个关键词试试", nil)
		}
		return
	}
	q := s.Shown
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("results-"+q)).TrackScroll(scrollOf(r)).Grow(1).Padding(0, pagePad, 0).Children(func() {
		ui.Text(c, fmt.Sprintf("“%s”的搜索结果", q)).FontSize(13).TextColor(p.muted).Margin(0, 0, 16)
		app.cards(c, core.Visible(r, n), source.LayoutGrid)
		app.feedFooter(c, r.Loading, r.ErrText(), r.More, retry)
	})
	app.pump(c, r)
}

// hotBoards shows the search home: its ranked sections as boards side by
// side, the others as rows of cards.
func (app *App) hotBoards(c *ui.Context) {
	s := &app.Search
	p := app.pal
	if !app.HasCap(source.CapSearchHome) {
		app.centered(c, iconSearch, "搜索", "输入关键词后按回车", nil)
		return
	}
	if s.Hot == nil {
		if s.HotErr != "" {
			app.centered(c, iconSearch, "搜索", "输入关键词后按回车", nil)
			return
		}
		sh := app.shimmer(c)
		ui.Row(c).Padding(0, pagePad).Gap(20).Role(ui.RoleStatus).Label("加载中").Children(func() {
			for i := range 2 {
				sh.bone(c.Key(i), 16).Size(320, 420)
			}
		})
		return
	}
	if s.HotFeeds == nil {
		s.HotFeeds = map[string]*Feed{}
	}
	items := func(sec Section) []Card {
		if len(sec.Items) == 0 && sec.Collection != "" {
			return app.SectionFeed(s.HotFeeds, app.Src, sec.Collection).Items
		}
		return sec.Items
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("hot")).Grow(1).Padding(0, pagePad, 28).Children(func() {
		ui.Row(c).Gap(20).AlignItems(ui.Start).Wrap().Children(func() {
			for bi, b := range s.Hot {
				if b.Layout != source.SectionRanked {
					continue
				}
				list := items(b)
				ui.Column(c.Key(bi)).Width(320).Padding(18, 14, 12).Radius(16).Background(p.surface).Gap(2).Children(func() {
					ui.Row(c).Gap(7).Padding(0, 6, 10).Children(func() {
						ui.Icon(c, iconFlame).Size(16, 16).TextColor(p.accent)
						ui.Text(c, b.Title).FontSize(15).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(2)
					})
					for i, it := range list {
						if i >= 10 {
							break
						}
						row := ui.ButtonBase(c.Key(it.ID)).Gap(12).Padding(8, 8).Radius(9).AlignItems(ui.Center).Label(it.Title).Description(joinNonEmpty(fmt.Sprintf("第%d名", i+1), it.Note))
						pressable(row, ui.Transparent, p.hover.Mix(p.text, 0.02), 0.98)
						row.Children(func() {
							n := ui.Text(c, itoa(i+1)).FontSize(15).Bold().Width(18).TextAlign(ui.Center)
							n.TextColor(ternary(i < 3, p.accent, p.faint))
							ui.Text(c, it.Title).FontSize(13.5).SingleLine().Grow(1).TextColor(p.text)
							if it.Note != "" {
								ui.Text(c, it.Note).FontSize(11).TextColor(p.muted).NoWrap()
							}
						})
						if row.Clicked() {
							app.openCard(app.SourceID(), it)
						}
					}
				})
			}
		})
		for bi, b := range s.Hot {
			if b.Layout == source.SectionRanked {
				continue
			}
			list := items(b)
			if len(list) == 0 {
				continue
			}
			ui.Column(c.Key(fmt.Sprint("row", bi))).Margin(28, 0, 0).Children(func() {
				ui.Text(c, b.Title).FontSize(17).Bold().TextColor(p.text).Margin(0, 0, 12).Role(ui.RoleHeading).Level(2)
				shown := list
				if b.Layout != source.SectionGrid && len(shown) > n {
					shown = shown[:n]
				}
				app.cards(c, shown, source.LayoutGrid)
			})
		}
	})
}

// ---- Library ----

// The filters of the history page: everything, the dramas being watched,
// and those watched to the end.
var historyFilters = []string{"全部", "在看", "看完"}

// watchBadge says where a drama watched got to, over its cover.
func watchBadge(r *Record) string {
	if r.Done() {
		return "已看完"
	}
	return fmt.Sprintf("看到第%d集", r.Episode+1)
}

// watchBar draws how much of a drama was watched, under a card's title.
func (app *App) watchBar(c *ui.Context, r *Record) {
	p := app.pal
	frac := float32(0)
	if n := r.Episodes; n > 0 {
		done := float64(r.Episode)
		if r.Duration > 0 {
			done += min(r.Position/r.Duration, 1)
		}
		frac = float32(done / float64(n))
	}
	ui.Box(c).Height(3).Radius(2).Background(p.hairline).Margin(2, 0).Draw(func(pt *ui.Painter, rc ui.Rect) {
		pt.Fill(ui.Rect{X: rc.X, Y: rc.Y, W: rc.W * min(max(frac, 0.02), 1), H: rc.H}, p.accent, 2)
	})
}

func (app *App) historyPage(c *ui.Context) {
	p := app.pal
	all := app.Lib.Recent()
	app.header(c, "继续观看", "从上次停下的地方接着看", nil)
	if len(all) == 0 {
		app.centered(c, iconClock, "还没有观看记录", "看过的短剧会出现在这里", nil)
		return
	}
	ui.Row(c).Padding(0, pagePad, 18).Children(func() {
		choices(c, "筛选观看记录").Gap(8).Children(func() {
			for i, name := range historyFilters {
				label := fmt.Sprintf("%s %d", name, len(core.HistoryShown(all, i)))
				on := i == app.historyFilter
				if radio(app.chip(c.Key(i), label, on), on).Label(name).Clicked() && !on {
					app.historyFilter = i
					app.shelf = ui.ScrollState{}
				}
			}
		})
	})
	recent := core.HistoryShown(all, app.historyFilter)
	if len(recent) == 0 {
		app.centered(c, iconClock, ternary(app.historyFilter == 2, "还没有看完的剧", "没有在看的剧"), "", nil)
		return
	}
	cards := make([]Card, len(recent))
	for i, w := range recent {
		cards[i] = w.Card
	}
	ui.Scroll(c.Key("history")).TrackScroll(&app.shelf).Grow(1).Padding(4, pagePad, 24).Children(func() {
		app.grid(c, cards, func(i int, s Card) cardExtras {
			w := recent[i]
			return cardExtras{
				badge: watchBadge(w),
				under: func() string {
					app.watchBar(c, w)
					return app.recordLine(c, w.Source, ago(w))
				},
				corner: func() {
					x := ui.ButtonBase(c.Key("forget")).Label("移除“" + s.Title + "”的记录")
					x.Children(func() {
						ui.Text(c, "移除").FontSize(11).TextColor(x.AnimateColor("fg", ternary(x.Hovered(), p.accent, p.faint), quick))
					})
					if x.Clicked() {
						app.Lib.Forget(w.Ref())
					}
				},
				onClick: func() { app.Open(w.Source, w.Card, -1) },
			}
		})
	})
}

func (app *App) followsPage(c *ui.Context) {
	followed := app.Lib.Followed()
	app.header(c, "追剧", "收藏的短剧都在这里", nil)
	if len(followed) == 0 {
		app.centered(c, iconHeart, "还没有追的剧", "在播放页点“追剧”即可收藏", nil)
		return
	}
	cards := make([]Card, len(followed))
	for i, fl := range followed {
		cards[i] = fl.Card
	}
	ui.Scroll(c.Key("follows")).Grow(1).Padding(4, pagePad, 24).Children(func() {
		app.grid(c, cards, func(i int, s Card) cardExtras {
			fl := followed[i]
			w := app.Lib.History[fl.Ref()]
			if w == nil {
				return cardExtras{
					badge:   cardBadge(s),
					under:   func() string { return app.recordLine(c, fl.Source, "还没看") },
					onClick: func() { app.Open(fl.Source, fl.Card, -1) },
				}
			}
			return cardExtras{
				badge: watchBadge(w),
				under: func() string {
					app.watchBar(c, w)
					return app.recordLine(c, fl.Source, ago(w))
				},
				onClick: func() { app.Open(fl.Source, fl.Card, -1) },
			}
		})
	})
}

// recordLine says under a record's card which source it is of, marked
// when it can't open now, and what else there is to say; it returns
// what screen readers read of it.
func (app *App) recordLine(c *ui.Context, sourceID, rest string) string {
	p := app.pal
	name := app.SourceName(sourceID)
	off := !app.Available(sourceID)
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		src := ui.Text(c, name).FontSize(11).FontWeight(500).SingleLine().Shrink(1)
		if off {
			src.TextColor(p.faint)
			ui.Text(c, "不可用").FontSize(10).FontWeight(600).TextColor(p.muted).Padding(0, 4).Radius(4).Background(p.surface).NoWrap()
		} else {
			src.TextColor(p.muted)
		}
		ui.Text(c, "·").FontSize(11).TextColor(p.faint)
		ui.Text(c, rest).FontSize(11).TextColor(p.muted).NoWrap()
	})
	return joinNonEmpty(name, ternary(off, "片源不可用", ""), rest)
}

// unavailableDialog tells that a record's source can't open it now, and
// leads to where it can be turned on.
func (app *App) unavailableDialog(c *ui.Context) {
	u := app.Unavailable
	if u != app.unavail {
		app.unavail, app.unavailOpen = u, u != nil
	}
	if u == nil {
		return
	}
	if !app.unavailOpen { // closed with Esc or a click beside it
		app.Unavailable, app.unavail = nil, nil
		return
	}
	p := app.pal
	detail := fmt.Sprintf("“%s”来自片源“%s”，这个片源已关闭。打开它就能接着看，观看记录都还在。", u.Title, u.Name)
	if !u.Installed {
		detail = fmt.Sprintf("“%s”来自片源“%s”，这个片源还没有安装。装上以后，观看记录会自动对上。", u.Title, u.Name)
	}
	ui.DialogBase(c, &app.unavailOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.Width(400).Radius(16).Background(p.bg).Padding(24).Gap(12).Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.3)).
			Label("片源不可用")
		if p.dark {
			panel.Border(1, p.hairline)
		}
		ui.Text(c, "片源不可用").FontSize(17).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(2)
		ui.Text(c, detail).FontSize(13.5).LineHeight(1.5).TextColor(p.muted)
		ui.Row(c).Gap(10).Justify(ui.End).Margin(8, 0, 0).Children(func() {
			app.secondary(c, nil, "好").OnClick(func() { app.Unavailable = nil })
			app.primary(c, nil, "管理片源").OnClick(func() {
				app.Unavailable = nil
				app.CloseWatch()
				app.Go(core.PageSources)
			})
		})
	})
}
