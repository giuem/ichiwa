package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	pagePad  = 32
	cardMinW = 156
	cardGap  = 20
	// titleLine is the line height of a card's title. Titles always take
	// two lines of room so cards in a row line up whatever their length.
	titleLine = 18
)

func itoa(n int) string { return strconv.Itoa(n) }

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
	if title != app.told {
		app.told = title
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
	// under draws below the title, in place of the tag, and returns what
	// screen readers read of it.
	under func() string
	// corner draws controls of their own at the card's bottom right, which
	// a card, being one button, could not hold inside it.
	corner func()
}

func episodesExtras(_ int, s Show) cardExtras { return cardExtras{badge: episodesBadge(s)} }

// card is a drama in a grid: its cover and title, and the page's extras. A
// click opens it.
func (app *App) card(c *ui.Context, s Show, width float32, x cardExtras) {
	p := app.pal
	ui.Box(c.Key(s.ID)).Width(width).Children(func() {
		b := ui.ButtonBase(c).Width(width).AlignItems(ui.Start).Cursor(ui.CursorPointer).Label(s.Title)
		hover := b.Hovered() || b.FocusVisible()
		lift := b.AnimateTo("lift", boolf(hover), ui.Smooth)
		b.AnimatePose(ui.Pose{Scale: 1 - 0.02*boolf(b.Pressed())}, ui.Snappy)
		note := s.Tag
		b.Children(func() {
			ui.Column(c).Width(width).Gap(9).Children(func() {
				poster := app.poster(c, s.Cover, s.Title, 10)
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
					} else if s.Tag != "" {
						ui.Text(c, s.Tag).FontSize(11.5).TextColor(p.muted).SingleLine()
					}
				})
			})
		})
		b.Description(joinNonEmpty(x.badge, note))
		if b.Clicked() {
			app.open(s, -1)
		}
		if x.corner != nil {
			ui.Box(c).Absolute().Right(0).Bottom(0).Children(x.corner)
		}
	})
}

// joinNonEmpty joins what there is of parts, as one line to read.
func joinNonEmpty(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "，")
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
func (app *App) grid(c *ui.Context, shows []Show, extras func(i int, s Show) cardExtras) {
	n, w := columns(app.contentWidth(c))
	ui.Column(c).Gap(28).Children(func() {
		for start := 0; start < len(shows); start += n {
			ui.Row(c.Key(start)).Gap(cardGap).AlignItems(ui.Start).Children(func() {
				for i := start; i < min(start+n, len(shows)); i++ {
					app.card(c, shows[i], w, extras(i, shows[i]))
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
		app.grid(c, visible(f, n), episodesExtras)
		app.feedFooter(c, f.loading, f.err, f.more, retry)
	})
	app.pump(c, f)
}

// ---- Ranking ----

type rankState struct {
	page    *RankPage // the last page loaded: the boards, and which is shown
	list    Feed
	actors  []Actor // on the actors' boards, which list no dramas
	content int
	board   int
}

// loadRank loads a ranking from its start: content and board are IDs, empty
// for the default.
func (app *App) loadRank(content, board string) {
	r := &app.rank
	r.list.reset()
	r.actors = nil
	r.list.fetch = func(cursor string) (feedPage, error) {
		p, err := app.be.Rank(content, board, cursor)
		if err != nil {
			return feedPage{}, err
		}
		return feedPage{items: p.Items, cursor: p.Cursor, more: p.HasMore, extra: p}, nil
	}
	r.list.onPage = r.loaded
	app.loadMore(&r.list)
}

// loaded takes in a page of the ranking: its boards, and its actors.
func (r *rankState) loaded(fp feedPage) {
	p := fp.extra.(*RankPage)
	r.page = p
	if len(p.Actors) > 0 {
		before := len(r.actors)
		r.addActors(p.Actors)
		// The feed holds dramas and took a page of only actors for its
		// end; it ends when a page brings no new actors.
		fresh := len(r.actors) > before
		r.list.more = fresh && p.HasMore && p.Cursor != ""
		if fresh {
			r.list.stalls = 0
		}
	}
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
		ui.Row(c).Gap(22).Role(ui.RoleTabList).Label("榜单类别").FocusGroup(ui.Horizontal).Children(func() {
			for i, ct := range contents {
				if app.tab(c.Key("tab-"+ct.ID), ct.Name, i == r.content).Clicked() && i != r.content {
					r.content, r.board = i, 0
					app.loadRank(ct.ID, "")
				}
			}
		})
		if r.content < len(contents) {
			boards := contents[r.content].Boards
			choices(c, "榜单").Gap(8).Wrap().Children(func() {
				for j, b := range boards {
					if radio(app.chip(c.Key(b.ID), b.Name, j == r.board), j == r.board).Clicked() && j != r.board {
						r.board = j
						app.loadRank(contents[r.content].ID, b.ID)
					}
				}
			})
		}
	})
	if len(r.actors) > 0 {
		ui.Scroll(c.Key("rank-actors")).TrackScroll(&l.scroll).Grow(1).Padding(4, pagePad, 0).Children(func() {
			app.actorGrid(c, r.actors)
			app.feedFooter(c, l.loading, l.err, l.more, retry)
		})
		app.pump(c, l)
		return
	}
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
		app.grid(c, visible(l, n), func(i int, s Show) cardExtras {
			return cardExtras{under: func() string {
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
				return joinNonEmpty(fmt.Sprintf("第%d名", i+1), s.Tag)
			}}
		})
		app.feedFooter(c, l.loading, l.err, l.more, retry)
	})
	app.pump(c, l)
}

// addActors adds the actors not listed yet.
func (r *rankState) addActors(actors []Actor) {
	for _, a := range actors {
		if !slices.ContainsFunc(r.actors, func(b Actor) bool { return b.ID == a.ID }) {
			r.actors = append(r.actors, a)
		}
	}
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
	told    string // the query screen readers were told the results of
}

// runSearch shows the results of q, or the hot boards when q is empty.
func (app *App) runSearch(q string) {
	s := &app.search
	q = strings.TrimSpace(q)
	f := &s.results
	f.reset()
	s.shown, s.told = q, ""
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
	ui.Row(c).Padding(0, pagePad, 22).Role(ui.RoleSearch).Children(func() {
		f := ui.SearchField(c.Key("search"), &s.query).Label("搜索短剧").Placeholder("搜索剧名、演员、题材，回车搜索").Bind(&s.field).
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
	// The focus stays in the field, so nothing else says the search is done.
	if r.loaded && !r.loading && r.err == "" && s.told != s.shown {
		s.told = s.shown
		c.Announce(ternary(len(r.items) > 0, "“"+s.shown+"”的搜索结果已显示", "没有找到“"+s.shown+"”"))
	}
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
		app.grid(c, visible(r, n), episodesExtras)
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
		ui.Row(c).Padding(0, pagePad).Gap(20).Role(ui.RoleStatus).Label("加载中").Children(func() {
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
						ui.Text(c, b.Name).FontSize(15).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(2)
					})
					for i, it := range b.Items {
						if i >= 10 {
							break
						}
						row := ui.ButtonBase(c.Key(it.ID)).Gap(12).Padding(8, 8).Radius(9).AlignItems(ui.Center).Label(it.Title).Description(joinNonEmpty(fmt.Sprintf("第%d名", i+1), it.Tag))
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

// The filters of the history page: everything, the dramas being watched,
// and those watched to the end.
var historyFilters = []string{"全部", "在看", "看完"}

// historyShown is the records the history filter f shows, latest first.
func historyShown(recent []*Record, f int) []*Record {
	if f == 0 {
		return recent
	}
	out := make([]*Record, 0, len(recent))
	for _, r := range recent {
		if r.Done() == (f == 2) {
			out = append(out, r)
		}
	}
	return out
}

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
	if n := r.Show.Episodes; n > 0 {
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
	all := app.lib.Recent()
	app.header(c, "继续观看", "从上次停下的地方接着看", nil)
	if len(all) == 0 {
		app.centered(c, iconClock, "还没有观看记录", "看过的短剧会出现在这里", nil)
		return
	}
	ui.Row(c).Padding(0, pagePad, 18).Children(func() {
		choices(c, "筛选观看记录").Gap(8).Children(func() {
			for i, name := range historyFilters {
				label := fmt.Sprintf("%s %d", name, len(historyShown(all, i)))
				on := i == app.historyFilter
				if radio(app.chip(c.Key(i), label, on), on).Label(name).Clicked() && !on {
					app.historyFilter = i
					app.shelf = ui.ScrollState{}
				}
			}
		})
	})
	recent := historyShown(all, app.historyFilter)
	if len(recent) == 0 {
		app.centered(c, iconClock, ternary(app.historyFilter == 2, "还没有看完的剧", "没有在看的剧"), "", nil)
		return
	}
	shows := make([]Show, len(recent))
	for i, w := range recent {
		shows[i] = w.Show
	}
	ui.Scroll(c.Key("history")).TrackScroll(&app.shelf).Grow(1).Padding(4, pagePad, 24).Children(func() {
		app.grid(c, shows, func(i int, s Show) cardExtras {
			w := recent[i]
			return cardExtras{
				badge: watchBadge(w),
				under: func() string {
					app.watchBar(c, w)
					ui.Text(c, ago(w)).FontSize(11).TextColor(p.muted)
					return ago(w)
				},
				corner: func() {
					x := ui.ButtonBase(c.Key("forget")).Label("移除“" + s.Title + "”的记录")
					x.Children(func() {
						ui.Text(c, "移除").FontSize(11).TextColor(x.AnimateColor("fg", ternary(x.Hovered(), p.accent, p.faint), quick))
					})
					if x.Clicked() {
						app.lib.Forget(s.ID)
					}
				},
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
		app.grid(c, shows, func(i int, s Show) cardExtras {
			w := app.lib.History[s.ID]
			if w == nil {
				return cardExtras{badge: episodesBadge(s), under: func() string {
					ui.Text(c, "还没看").FontSize(11).TextColor(app.pal.muted)
					return "还没看"
				}}
			}
			return cardExtras{badge: watchBadge(w), under: func() string {
				app.watchBar(c, w)
				ui.Text(c, ago(w)).FontSize(11).TextColor(app.pal.muted)
				return ago(w)
			}}
		})
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
