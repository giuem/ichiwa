package catalog

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

// Season is one season of a multi-season drama.
type Season struct {
	Show
	Index  int  // from 1
	Online bool // it has episodes to watch; an announced season has none

	// What the 上新 calendar says of the latest season: New when it is
	// just out, Note when it comes out ("预计11月10日上线"), and Heat how
	// many reserved it or how hot it is.
	New  bool
	Note string
	Heat string
}

// Seasons lists the seasons of the drama id belongs to, in order, or none
// when it has only the one.
func (b *Backend) Seasons(id string) ([]Season, error) {
	ctx, cancel := ctxTimeout(45 * time.Second)
	defer cancel()
	album, err := b.client.Seasons(ctx, id)
	if err != nil {
		return nil, err
	}
	out := seasonsOf(album.Seasons)
	if len(out) < 2 {
		return nil, nil
	}
	return out, nil
}

// seasonsOf orders the seasons, drops repeats, and numbers the ones the
// server left unnumbered by their place.
func seasonsOf(in []model.Season) []Season {
	seen := map[string]bool{}
	var out []Season
	for _, s := range in {
		if s.SeriesID == "" || s.SeriesID == "0" || seen[s.SeriesID] {
			continue
		}
		seen[s.SeriesID] = true
		out = append(out, Season{
			Show:   Show{ID: s.SeriesID, Title: s.Title, Cover: s.CoverURL, Episodes: s.EpisodeCount},
			Index:  s.Index,
			Online: s.EpisodeCount > 0,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Index, out[j].Index
		if a == 0 || b == 0 {
			return a != 0 && b == 0 // unnumbered ones last
		}
		return a < b
	})
	prev := 0
	for i := range out {
		if out[i].Index <= prev {
			out[i].Index = prev + 1
		}
		prev = out[i].Index
	}
	return out
}

// annotate copies what the calendar says of a season onto it.
func annotate(s *Season, r *model.NewRelease, loc *time.Location) {
	s.Heat = r.Heat
	if s.Cover == "" {
		s.Cover = r.CoverURL
	}
	if s.Desc == "" {
		s.Desc = r.Description
	}
	if r.Online {
		s.New = true
		if !r.PublishAt.IsZero() {
			s.Note = r.PublishAt.In(loc).Format("1月2日") + "上新"
		}
		return
	}
	switch {
	case r.Note != "":
		s.Note = r.Note
	case !r.PublishAt.IsZero():
		s.Note = "预计" + r.PublishAt.In(loc).Format("1月2日 15:04") + "上线"
	}
}

// calendarDays is how many days of the calendar a season is looked for
// on, besides today.
const calendarDays = 7

// calendarPages is how many pages of a day are read at most.
const calendarPages = 3

// Announce looks up on the 上新 calendar the seasons not out yet, for when
// they come out, and the latest season, for whether it just did. It
// returns the seasons with what it found, reading no further days once it
// found them all.
func (b *Backend) Announce(seasons []Season) []Season {
	ctx, cancel := ctxTimeout(60 * time.Second)
	defer cancel()
	out := slices.Clone(seasons)
	var pending []int
	for i, s := range out {
		if !s.Online || i == len(out)-1 {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return out
	}
	today, err := b.calendar(ctx, "")
	if err != nil {
		return out
	}
	look := func(items []model.NewRelease) {
		pending = slices.DeleteFunc(pending, func(i int) bool {
			r := matchRelease(items, out[i])
			if r != nil {
				annotate(&out[i], r, time.Local)
			}
			return r != nil
		})
	}
	waiting := func(online bool) bool {
		return slices.ContainsFunc(pending, func(i int) bool { return out[i].Online == online })
	}
	look(today.items)
	// Announced seasons on the days to come, the latest on the days gone.
	for _, online := range []bool{false, true} {
		for _, day := range calendarDates(today.dates, today.date, online) {
			if !waiting(online) {
				break
			}
			if d, err := b.calendar(ctx, day); err == nil {
				look(d.items)
			}
		}
	}
	return out
}

type calDay struct {
	date  string
	dates []string // the days the calendar offers
	items []model.NewRelease
	at    time.Time
}

// calendar reads a day of the calendar, "" for today, keeping it ten
// minutes.
func (b *Backend) calendar(ctx context.Context, day string) (calDay, error) {
	b.mu.Lock()
	c, ok := b.days[day]
	b.mu.Unlock()
	if ok && time.Since(c.at) < 10*time.Minute {
		return c, nil
	}
	page, err := b.client.NewReleases(ctx, sdk.NewReleaseQuery{Date: day})
	if err != nil {
		return calDay{}, err
	}
	c = calDay{date: page.Date, dates: page.Dates, items: page.Items, at: time.Now()}
	for range calendarPages - 1 {
		if !page.HasMore || page.Cursor == "" {
			break
		}
		if page, err = b.client.NewReleases(ctx, sdk.NewReleaseQuery{Cursor: page.Cursor}); err != nil {
			break
		}
		c.items = append(c.items, page.Items...)
	}
	b.mu.Lock()
	if b.days == nil {
		b.days = map[string]calDay{}
	}
	b.days[day] = c
	b.mu.Unlock()
	return c, nil
}

// calendarDates are the days to look on besides today, nearest first: the
// days to come for a season not out, the days gone for one that is.
func calendarDates(dates []string, today string, online bool) []string {
	var out []string
	for _, d := range dates {
		if d == today || len(d) != 8 {
			continue
		}
		if online == (d < today) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	if online { // yesterday first
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if len(out) > calendarDays {
		out = out[:calendarDays]
	}
	return out
}

// matchRelease finds the season among the calendar's titles, by ID, or by
// title when the calendar names it under another ID.
func matchRelease(items []model.NewRelease, s Season) *model.NewRelease {
	for i := range items {
		if items[i].SeriesID == s.ID {
			return &items[i]
		}
	}
	title := normTitle(s.Title)
	if title == "" {
		return nil
	}
	for i := range items {
		if normTitle(items[i].Title) == title {
			return &items[i]
		}
	}
	return nil
}

func normTitle(t string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '　', '·', '・', '：', ':', '（', '）', '(', ')', '《', '》':
			return -1
		}
		return r
	}, t)
}
