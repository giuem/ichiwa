package hongguo

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"github.com/giuem/ichiwa/internal/source"
)

// defaultCalendarBudget is how long Media waits, once the details and the
// episodes are in, for the season list and its release notes.
const defaultCalendarBudget = time.Second

// seasonsTTL is how long a season list read in the background is reused.
const seasonsTTL = 10 * time.Minute

// calendarParallel bounds the calendar days read at once.
const calendarParallel = 4

type seasonsCache struct {
	entries []seasonEntry // nil for a show of a single season
	at      time.Time
}

// cachedSeasons returns a season list read in the last seasonsTTL.
func (s *Source) cachedSeasons(id string) ([]seasonEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.seasons[id]
	if !ok || time.Since(c.at) > seasonsTTL {
		return nil, false
	}
	return slices.Clone(c.entries), true
}

func (s *Source) storeSeasons(id string, entries []seasonEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seasons == nil {
		s.seasons = map[string]seasonsCache{}
	}
	s.seasons[id] = seasonsCache{entries: slices.Clone(entries), at: time.Now()}
}

// loadSeasons reads a show's seasons, then their release notes, in the
// background and on contexts of their own, so a slow list or calendar
// never holds up playback. Only the list itself is cached: the notes come
// from the calendar's days, which have a cache of their own that keeps
// only whole days, so a calendar that failed is read again next time.
// base gets the list as soon as it is in, annotated the list with its
// notes; both are closed without a value when the list cannot be read.
func (s *Source) loadSeasons(id string) (base, annotated <-chan []seasonEntry) {
	baseCh := make(chan []seasonEntry, 1)
	annCh := make(chan []seasonEntry, 1)
	go func() {
		entries, ok := s.cachedSeasons(id)
		if !ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			album, err := s.client.Seasons(ctx, id)
			cancel()
			if err != nil {
				close(baseCh)
				close(annCh)
				return
			}
			entries = seasonsOf(album.Seasons)
			if len(entries) < 2 {
				entries = nil
			}
			s.storeSeasons(id, entries)
		}
		baseCh <- entries
		if entries == nil {
			annCh <- nil
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		annCh <- s.announce(ctx, entries)
	}()
	return baseCh, annCh
}

// awaitSeasons waits up to the calendar budget for the annotated season
// list, settling for the plain list, or none, when the time is up.
func (s *Source) awaitSeasons(ctx context.Context, base, annotated <-chan []seasonEntry) []seasonEntry {
	budget := s.calendarBudget
	if budget <= 0 {
		budget = defaultCalendarBudget
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	var got []seasonEntry
	for {
		select {
		case e, ok := <-annotated:
			if ok {
				return e
			}
			return got
		case e, ok := <-base:
			if ok {
				got = e
			}
			base = nil
		case <-timer.C:
			return got
		case <-ctx.Done():
			return got
		}
	}
}

type seasonEntry struct {
	ID     string
	Title  string
	Cover  string
	Index  int
	Online bool
	New    bool
	Note   string
	Heat   string
}

type calFlight struct {
	done chan struct{}
	day  calDay
	err  error
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
		Name:      e.Title,
		Image:     e.Cover,
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
			Cover:  s.CoverURL,
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
	if s.Cover == "" {
		s.Cover = r.CoverURL
	}
	if s.Title == "" {
		s.Title = r.Title
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
	if len(pending) == 0 {
		return out
	}

	var days []string
	for _, online := range []bool{false, true} {
		if waiting(online) {
			days = append(days, calendarDates(today.dates, today.date, online)...)
		}
	}
	if len(days) == 0 {
		return out
	}

	results := make([]calDay, len(days))
	errs := make([]error, len(days))
	var wg sync.WaitGroup
	slots := make(chan struct{}, calendarParallel)
	for i, day := range days {
		wg.Add(1)
		go func(i int, day string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i], errs[i] = s.calendar(ctx, day)
		}(i, day)
	}
	wg.Wait()

	for i := range days {
		if len(pending) == 0 {
			break
		}
		if errs[i] == nil {
			look(results[i].items)
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
	if c, ok := s.days[day]; ok && time.Since(c.at) < 10*time.Minute {
		s.mu.Unlock()
		return c, nil
	}
	if fl, ok := s.calInflight[day]; ok {
		s.mu.Unlock()
		select {
		case <-fl.done:
			return fl.day, fl.err
		case <-ctx.Done():
			return calDay{}, ctx.Err()
		}
	}
	fl := &calFlight{done: make(chan struct{})}
	if s.calInflight == nil {
		s.calInflight = map[string]*calFlight{}
	}
	s.calInflight[day] = fl
	s.mu.Unlock()

	c, complete, err := s.fetchCalendarDay(ctx, day)

	s.mu.Lock()
	delete(s.calInflight, day)
	if err == nil && complete {
		if s.days == nil {
			s.days = map[string]calDay{}
		}
		s.days[day] = c
	}
	s.mu.Unlock()

	fl.day, fl.err = c, err
	close(fl.done)
	return c, err
}

func (s *Source) fetchCalendarDay(ctx context.Context, day string) (calDay, bool, error) {
	page, err := s.client.NewReleases(ctx, sdk.NewReleaseQuery{Date: day})
	if err != nil {
		return calDay{}, false, err
	}
	c := calDay{date: page.Date, dates: page.Dates, items: page.Items}
	for range calendarPages - 1 {
		if !page.HasMore || page.Cursor == "" {
			break
		}
		next, err := s.client.NewReleases(ctx, sdk.NewReleaseQuery{Cursor: page.Cursor})
		if err != nil {
			// A half-read day must not be cached.
			return c, false, nil
		}
		page = next
		c.items = append(c.items, page.Items...)
	}
	c.at = time.Now()
	return c, true, nil
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
