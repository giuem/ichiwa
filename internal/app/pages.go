package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	pagePad  = 32
	cardMinW = 156
	cardGap  = 20
)

func itoa(n int) string { return strconv.Itoa(n) }

// header draws a page's title row, which also drags the window.
func (app *App) header(c *ui.Context, title, subtitle string, trailing func()) {
	p := app.pal
	bar := c.TitleBar()
	ui.Row(c).Padding(max(bar.Height, 28)+10, pagePad, 18, pagePad).Gap(12).AlignItems(ui.End).DragWindow().Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(28).Bold().TextColor(p.text)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(13).TextColor(p.muted)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}

// centered shows a message in the middle of a page, with a button that
// tries again when retry is set.
func (app *App) centered(c *ui.Context, icon *ui.SVG, title, detail string, retry func()) {
	p := app.pal
	ui.Column(c).Grow(1).Center().Gap(10).Padding(40).Children(func() {
		ui.Box(c).Size(64, 64).Radius(999).Background(p.surface).Center().Margin(0, 0, 6).Children(func() {
			ui.Icon(c, icon).Size(26, 26).TextColor(p.muted)
		})
		ui.Text(c, title).FontSize(16).Bold().TextColor(p.text)
		if detail != "" {
			ui.Text(c, detail).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
		}
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

// poster draws a cover, fading it in over a placeholder as it loads.
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
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Opacity(1 - in).Children(func() {
				ui.Text(c, firstRune(title)).FontSize(26).Bold().TextColor(p.faint)
			})
		}
		if bmp != nil {
			img := ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Label(title)
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

// card is a drama in a grid: its cover and title, a badge over the cover,
// and what under draws below the title. A click opens it.
func (app *App) card(c *ui.Context, s Show, width float32, badge string, under func()) {
	p := app.pal
	col := ui.Column(c.Key(s.ID)).Width(width).Gap(9).Cursor(ui.CursorPointer).Label(s.Title)
	hover := col.Hovered()
	lift := col.AnimateTo("lift", boolf(hover), ui.Smooth)
	col.AnimatePose(ui.Pose{Scale: 1 - 0.02*boolf(col.Pressed())}, ui.Snappy)
	col.Children(func() {
		poster := app.poster(c, s.Cover, s.Title, 10)
		poster.Shadow(0, 2+8*lift, 6+18*lift, 0, p.shade.Alpha(0.10+0.16*lift)).Translate(0, -3*lift)
		poster.Children(func() {
			if badge != "" {
				ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(white).
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
			ui.Text(c, s.Title).FontSize(13.5).FontWeight(600).MaxLines(2).
				TextColor(col.AnimateColor("title", ternary(hover, p.accent, p.text), quick))
			if under != nil {
				under()
			} else if s.Tag != "" {
				ui.Text(c, s.Tag).FontSize(11.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	if col.Clicked() {
		app.open(s, -1)
	}
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func episodesBadge(s Show) string {
	if s.Episodes > 0 {
		return fmt.Sprintf("全%d集", s.Episodes)
	}
	return ""
}

// grid lays cards out in rows that fill the width.
func (app *App) grid(c *ui.Context, shows []Show, badge func(i int, s Show) string, under func(i int, s Show) func()) {
	n, w := columns(app.contentWidth(c))
	ui.Column(c).Gap(28).Children(func() {
		for start := 0; start < len(shows); start += n {
			ui.Row(c.Key(start)).Gap(cardGap).AlignItems(ui.Start).Children(func() {
				for i := start; i < min(start+n, len(shows)); i++ {
					var u func()
					if under != nil {
						u = under(i, shows[i])
					}
					app.card(c, shows[i], w, badge(i, shows[i]), u)
				}
			})
		}
	})
}

// skeletonGrid stands in for a grid while it loads.
func (app *App) skeletonGrid(c *ui.Context, rows int) {
	s := app.shimmer(c)
	n, w := columns(app.contentWidth(c))
	ui.Column(c).Grow(1).Padding(4, pagePad, 0).Gap(28).ClipY().Children(func() {
		for r := range rows {
			ui.Row(c.Key(r)).Gap(cardGap).Children(func() {
				for i := range n {
					ui.Column(c.Key(i)).Width(w).Gap(10).Children(func() {
						s.bone(c, 10).AspectRatio(3.0 / 4)
						s.bone(c, 4).Height(13).WidthPercent(float32(60 + (r*n+i)*17%35))
						s.bone(c, 4).Height(10).WidthPercent(36)
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
			ui.Text(c, err).FontSize(12.5).TextColor(p.muted)
			app.secondary(c, iconRefresh, "重试").OnClick(retry)
		case !more:
			ui.Box(c).Width(28).Height(1).Background(p.hairline)
			ui.Text(c, "已经到底了").FontSize(12).TextColor(p.faint)
			ui.Box(c).Width(28).Height(1).Background(p.hairline)
		}
	})
}

func (app *App) feedPage(c *ui.Context, channel string) {
	f := app.feed(channel)
	if !f.loaded && f.err == "" {
		app.loadMore(f)
	}
	subtitle := map[string]string{
		pageRecommend: "为你精选的热门短剧",
		pageLive:      "真人出演，好戏连台",
		pageComic:     "动态漫剧，脑洞大开",
	}[channel]
	app.header(c, channel, subtitle, func() {
		if iconButton(c, iconRefresh, 16, app.pal.muted, "刷新").Clicked() {
			f.reset()
			app.loadMore(f)
		}
	})
	retry := func() { f.err = ""; app.loadMore(f) }
	switch {
	case len(f.items) == 0 && f.err != "":
		app.centered(c, iconX, "加载失败", f.err, retry)
		return
	case len(f.items) == 0:
		app.skeletonGrid(c, 3)
		return
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("feed-"+channel)).TrackScroll(&f.scroll).Grow(1).Padding(4, pagePad, 0).Children(func() {
		app.grid(c, visible(f, n), func(_ int, s Show) string { return episodesBadge(s) }, nil)
		app.feedFooter(c, f.loading, f.err, f.more, retry)
	})
	app.pump(c, f)
}

// ---- Ranking ----

type rankState struct {
	page    *RankPage // the last page loaded: the boards, and which is shown
	list    Feed
	content int
	board   int
}

// loadRank loads a ranking from its start: content and board are IDs, empty
// for the default.
func (app *App) loadRank(content, board string) {
	r := &app.rank
	r.list.reset()
	r.list.fetch = func(cursor string) (feedPage, error) {
		p, err := app.be.Rank(content, board, cursor)
		if err != nil {
			return feedPage{}, err
		}
		return feedPage{items: p.Items, cursor: p.Cursor, more: p.HasMore, extra: p}, nil
	}
	r.list.onPage = func(fp feedPage) {
		p := fp.extra.(*RankPage)
		r.page = p
		for i, ct := range p.Contents {
			if ct.ID == p.Content {
				r.content = i
				for j, b := range ct.Boards {
					if b.ID == p.Board {
						r.board = j
					}
				}
			}
		}
	}
	app.loadMore(&r.list)
}

func (app *App) rankPage(c *ui.Context) {
	r := &app.rank
	l := &r.list
	p := app.pal
	if l.fetch == nil {
		app.loadRank("", "")
	}
	retry := func() { l.err = ""; app.loadMore(l) }
	app.header(c, "排行榜", "看看大家都在追什么", nil)
	if r.page == nil {
		if l.err != "" {
			app.centered(c, iconX, "加载失败", l.err, retry)
		} else {
			app.skeletonGrid(c, 3)
		}
		return
	}
	contents := r.page.Contents
	ui.Column(c).Padding(0, pagePad, 18).Gap(14).Children(func() {
		ui.Row(c).Gap(22).Children(func() {
			for i, ct := range contents {
				if ct.Name == "演员" { // the actors' board lists people, not dramas
					continue
				}
				if app.tab(c.Key("tab-"+ct.ID), ct.Name, i == r.content).Clicked() && i != r.content {
					r.content, r.board = i, 0
					app.loadRank(ct.ID, "")
				}
			}
		})
		if r.content < len(contents) {
			boards := contents[r.content].Boards
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for j, b := range boards {
					if app.chip(c.Key(b.ID), b.Name, j == r.board).Clicked() && j != r.board {
						r.board = j
						app.loadRank(contents[r.content].ID, b.ID)
					}
				}
			})
		}
	})
	if len(l.items) == 0 {
		if l.err != "" {
			app.centered(c, iconX, "加载失败", l.err, retry)
		} else {
			app.skeletonGrid(c, 2)
		}
		return
	}
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("rank")).TrackScroll(&l.scroll).Grow(1).Padding(4, pagePad, 0).Children(func() {
		app.grid(c, visible(l, n), func(i int, s Show) string { return "" }, func(i int, s Show) func() {
			return func() {
				ui.Row(c).Gap(6).Children(func() {
					n := ui.Text(c, "TOP "+itoa(i+1)).FontSize(11).Bold()
					if i < 3 {
						n.TextColor(p.accent)
					} else {
						n.TextColor(p.muted)
					}
					if s.Tag != "" {
						ui.Text(c, s.Tag).FontSize(11).TextColor(p.muted).SingleLine()
					}
				})
			}
		})
		app.feedFooter(c, l.loading, l.err, l.more, retry)
	})
	app.pump(c, l)
}

// ---- Search ----

type searchState struct {
	query   string
	shown   string // the query the results are for
	results Feed
	hot     []HotBoard
	hotErr  string
	hotBusy bool
	focus   bool
	field   ui.Handle
}

// runSearch shows the results of q, or the hot boards when q is empty.
func (app *App) runSearch(q string) {
	s := &app.search
	q = strings.TrimSpace(q)
	f := &s.results
	f.reset()
	s.shown = q
	if q == "" {
		f.fetch = nil
		return
	}
	f.fetch = func(cursor string) (feedPage, error) {
		items, next, more, err := app.be.Search(q, cursor)
		return feedPage{items: items, cursor: next, more: more}, err
	}
	app.loadMore(f)
}

// loadHot loads the hot-search boards, once.
func (app *App) loadHot() {
	s := &app.search
	if s.hot != nil || s.hotBusy || s.hotErr != "" {
		return
	}
	s.hotBusy = true
	async(app, app.be.Hot, func(h []HotBoard, err error) {
		s.hotBusy = false
		if err != nil {
			s.hotErr = errText(err)
			return
		}
		s.hot = h
		if s.hot == nil {
			s.hot = []HotBoard{}
		}
	})
}

func (app *App) searchPage(c *ui.Context) {
	s := &app.search
	p := app.pal
	app.loadHot()
	app.header(c, "搜索", "", nil)
	ui.Row(c).Padding(0, pagePad, 22).Children(func() {
		f := ui.SearchField(c.Key("search"), &s.query).Placeholder("搜索剧名、演员、题材，回车搜索").Bind(&s.field).
			Grow(1).MaxWidth(560).Height(40).Radius(10).FontSize(14)
		if s.focus {
			s.field.Focus(c)
			s.focus = false
		}
		if f.Submitted() {
			app.runSearch(s.query)
		}
		if f.Changed() && strings.TrimSpace(s.query) == "" {
			app.runSearch("")
		}
	})
	if s.shown == "" {
		app.hotBoards(c)
		return
	}
	r := &s.results
	retry := func() { r.err = ""; app.loadMore(r) }
	if len(r.items) == 0 {
		switch {
		case r.loading:
			app.skeletonGrid(c, 2)
		case r.err != "":
			app.centered(c, iconX, "搜索失败", r.err, retry)
		default:
			app.centered(c, iconSearch, "没有找到“"+s.shown+"”", "换个关键词试试", nil)
		}
		return
	}
	q := s.shown
	n, _ := columns(app.contentWidth(c))
	ui.Scroll(c.Key("results-"+q)).TrackScroll(&r.scroll).Grow(1).Padding(0, pagePad, 0).Children(func() {
		ui.Text(c, fmt.Sprintf("“%s”的搜索结果", q)).FontSize(13).TextColor(p.muted).Margin(0, 0, 16)
		app.grid(c, visible(r, n), func(_ int, s Show) string { return episodesBadge(s) }, nil)
		app.feedFooter(c, r.loading, r.err, r.more, retry)
	})
	app.pump(c, r)
}

func (app *App) hotBoards(c *ui.Context) {
	s := &app.search
	p := app.pal
	if s.hot == nil {
		if s.hotErr != "" {
			app.centered(c, iconSearch, "搜索短剧", "输入剧名后按回车", nil)
			return
		}
		sh := app.shimmer(c)
		ui.Row(c).Padding(0, pagePad).Gap(20).Children(func() {
			for i := range 2 {
				sh.bone(c.Key(i), 16).Size(320, 420)
			}
		})
		return
	}
	ui.Scroll(c.Key("hot")).Grow(1).Padding(0, pagePad, 28).Children(func() {
		ui.Row(c).Gap(20).AlignItems(ui.Start).Wrap().Children(func() {
			for bi, b := range s.hot {
				ui.Column(c.Key(bi)).Width(320).Padding(18, 14, 12).Radius(16).Background(p.surface).Gap(2).Children(func() {
					ui.Row(c).Gap(7).Padding(0, 6, 10).Children(func() {
						ui.Icon(c, iconFlame).Size(16, 16).TextColor(p.accent)
						ui.Text(c, b.Name).FontSize(15).Bold().TextColor(p.text)
					})
					for i, it := range b.Items {
						if i >= 10 {
							break
						}
						row := ui.ButtonBase(c.Key(it.ID)).Gap(12).Padding(8, 8).Radius(9).AlignItems(ui.Center).FocusRing(false).Label(it.Title)
						pressable(row, ui.Transparent, p.hover.Mix(p.text, 0.02), 0.98)
						row.Children(func() {
							n := ui.Text(c, itoa(i+1)).FontSize(15).Bold().Width(18).TextAlign(ui.Center)
							n.TextColor(ternary(i < 3, p.accent, p.faint))
							ui.Text(c, it.Title).FontSize(13.5).SingleLine().Grow(1).TextColor(p.text)
							if it.Tag != "" {
								ui.Text(c, it.Tag).FontSize(11).TextColor(p.muted).NoWrap()
							}
						})
						if row.Clicked() {
							app.open(it, -1)
						}
					}
				})
			}
		})
	})
}

// ---- Library ----

func (app *App) historyPage(c *ui.Context) {
	p := app.pal
	recent := app.lib.Recent()
	app.header(c, "继续观看", "从上次停下的地方接着看", nil)
	if len(recent) == 0 {
		app.centered(c, iconClock, "还没有观看记录", "看过的短剧会出现在这里", nil)
		return
	}
	shows := make([]Show, len(recent))
	for i, w := range recent {
		shows[i] = w.Show
	}
	ui.Scroll(c.Key("history")).TrackScroll(&app.shelf).Grow(1).Padding(4, pagePad, 24).Children(func() {
		app.grid(c, shows, func(i int, s Show) string {
			return fmt.Sprintf("看到第%d集", recent[i].Episode+1)
		}, func(i int, s Show) func() {
			w := recent[i]
			return func() {
				frac := float32(0)
				if s.Episodes > 0 {
					done := float64(w.Episode)
					if w.Duration > 0 {
						done += w.Position / w.Duration
					}
					frac = float32(done / float64(s.Episodes))
				}
				ui.Box(c).Height(3).Radius(2).Background(p.hairline).Margin(2, 0).Draw(func(pt *ui.Painter, r ui.Rect) {
					pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * min(max(frac, 0.02), 1), H: r.H}, p.accent, 2)
				})
				ui.Row(c).Children(func() {
					ui.Text(c, ago(w)).FontSize(11).TextColor(p.muted).Grow(1)
					x := ui.ButtonBase(c.Key("forget")).FocusRing(false).Label("移除记录")
					x.Children(func() {
						ui.Text(c, "移除").FontSize(11).TextColor(x.AnimateColor("fg", ternary(x.Hovered(), p.accent, p.faint), quick))
					})
					if x.Clicked() {
						app.lib.Forget(s.ID)
					}
				})
			}
		})
	})
}

func (app *App) followsPage(c *ui.Context) {
	shows := app.lib.Followed()
	app.header(c, "追剧", "收藏的短剧都在这里", nil)
	if len(shows) == 0 {
		app.centered(c, iconHeart, "还没有追的剧", "在播放页点“追剧”即可收藏", nil)
		return
	}
	ui.Scroll(c.Key("follows")).Grow(1).Padding(4, pagePad, 24).Children(func() {
		app.grid(c, shows, func(_ int, s Show) string {
			if w := app.lib.History[s.ID]; w != nil {
				return fmt.Sprintf("看到第%d集", w.Episode+1)
			}
			return episodesBadge(s)
		}, nil)
	})
}

func (app *App) toggleFollow(s Show) {
	app.lib.ToggleFollow(s)
	if app.lib.Following(s.ID) {
		app.toast = "已加入追剧"
	} else {
		app.toast = "已取消追剧"
	}
}
