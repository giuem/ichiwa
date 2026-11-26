package hongguo

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"github.com/giuem/ichiwa/internal/source"
)

type seasonEntry struct {
	ID     string
	Title  string
	Index  int
	Online bool
	New    bool
	Note   string
	Heat   string
}

func (s *Source) loadSeasons(ctx context.Context, id string) []source.Season {
	album, err := s.client.Seasons(ctx, id)
	if err != nil {
		return nil
	}
	entries := seasonsOf(album.Seasons)
	if len(entries) < 2 {
		return nil
	}
	entries = s.announce(ctx, entries)
	out := make([]source.Season, 0, len(entries))
	for _, e := range entries {
		out = append(out, toProtocolSeason(e, e.ID == id))
	}
	return out
}

func toProtocolSeason(e seasonEntry, current bool) source.Season {
	var parts []string
	switch {
	case !e.Online:
		if e.Note != "" {
			parts = append(parts, e.Note)
		}
	case e.New:
		if e.Note != "" {
			parts = append(parts, e.Note)
		} else {
			parts = append(parts, "新上线")
		}
	}
	if (!e.Online || e.New) && e.Heat != "" {
		parts = append(parts, e.Heat)
	}
	return source.Season{
		ID:        e.ID,
		Title:     fmt.Sprintf("第%d季", e.Index),
		Current:   current,
		Available: source.Bool(e.Online),
		Note:      strings.Join(parts, " · "),
	}
}

// seasonsOf orders the seasons, drops repeats, and numbers the ones the
// server left unnumbered by their place.
func seasonsOf(in []model.Season) []seasonEntry {
	seen := map[string]bool{}
	var out []seasonEntry
	for _, s := range in {
		if s.SeriesID == "" || s.SeriesID == "0" || seen[s.SeriesID] {
			continue
		}
		seen[s.SeriesID] = true
		out = append(out, seasonEntry{
			ID:     s.SeriesID,
			Title:  s.Title,
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

func annotate(s *seasonEntry, r *model.NewRelease, loc *time.Location) {
	s.Heat = r.Heat
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

const (
	calendarDays  = 7
	calendarPages = 3
)

func (s *Source) announce(ctx context.Context, seasons []seasonEntry) []seasonEntry {
	out := slices.Clone(seasons)
	var pending []int
	for i, se := range out {
		if !se.Online || i == len(out)-1 {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return out
	}
	today, err := s.calendar(ctx, "")
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
	for _, online := range []bool{false, true} {
		for _, day := range calendarDates(today.dates, today.date, online) {
			if !waiting(online) {
				break
			}
			if d, err := s.calendar(ctx, day); err == nil {
				look(d.items)
			}
		}
	}
	return out
}

type calDay struct {
	date  string
	dates []string
	items []model.NewRelease
	at    time.Time
}

func (s *Source) calendar(ctx context.Context, day string) (calDay, error) {
	s.mu.Lock()
	c, ok := s.days[day]
	s.mu.Unlock()
	if ok && time.Since(c.at) < 10*time.Minute {
		return c, nil
	}
	page, err := s.client.NewReleases(ctx, sdk.NewReleaseQuery{Date: day})
	if err != nil {
		return calDay{}, err
	}
	c = calDay{date: page.Date, dates: page.Dates, items: page.Items, at: time.Now()}
	for range calendarPages - 1 {
		if !page.HasMore || page.Cursor == "" {
			break
		}
		if page, err = s.client.NewReleases(ctx, sdk.NewReleaseQuery{Cursor: page.Cursor}); err != nil {
			break
		}
		c.items = append(c.items, page.Items...)
	}
	s.mu.Lock()
	if s.days == nil {
		s.days = map[string]calDay{}
	}
	s.days[day] = c
	s.mu.Unlock()
	return c, nil
}

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
	if online {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	if len(out) > calendarDays {
		out = out[:calendarDays]
	}
	return out
}

func matchRelease(items []model.NewRelease, s seasonEntry) *model.NewRelease {
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
