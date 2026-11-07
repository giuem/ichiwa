package catalog

import (
	"slices"
	"testing"
	"time"

	"github.com/beilunyang/openhongguo/sdk/model"
)

// Seasons come in order, once each, numbered even when the server left
// some unnumbered, and an announced season is not online.
func TestSeasonsOf(t *testing.T) {
	got := seasonsOf([]model.Season{
		{SeriesID: "3", Index: 3},
		{SeriesID: "1", Index: 1, EpisodeCount: 80},
		{SeriesID: "x"},
		{SeriesID: "1", Index: 1},
		{SeriesID: "", Index: 2},
		{SeriesID: "2", Index: 2, EpisodeCount: 60},
	})
	var ids []string
	var idx []int
	for _, s := range got {
		ids, idx = append(ids, s.ID), append(idx, s.Index)
	}
	if !slices.Equal(ids, []string{"1", "2", "3", "x"}) || !slices.Equal(idx, []int{1, 2, 3, 4}) {
		t.Fatalf("seasonsOf = %v %v", ids, idx)
	}
	if !got[0].Online || got[2].Online {
		t.Errorf("online = %v, %v", got[0].Online, got[2].Online)
	}
}

// An announced season is looked for on the days to come, one out on the
// days gone, nearest first, never more than a week.
func TestCalendarDates(t *testing.T) {
	var dates []string
	for d := 1; d <= 30; d++ {
		dates = append(dates, time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC).Format("20060102"))
	}
	if got := calendarDates(dates, "20261108", false); !slices.Equal(got, []string{"20261109", "20261110", "20261111", "20261112", "20261113", "20261114", "20261115"}) {
		t.Errorf("announced: %v", got)
	}
	if got := calendarDates(dates[:10], "20261108", true); !slices.Equal(got, []string{"20261107", "20261106", "20261105", "20261104", "20261103", "20261102", "20261101"}) {
		t.Errorf("out: %v", got)
	}
}

// The calendar names a season by its ID, or else by its title.
func TestMatchRelease(t *testing.T) {
	items := []model.NewRelease{{SeriesID: "9", Title: "别的剧"}, {SeriesID: "7", Title: "重生 第3季"}}
	if r := matchRelease(items, Season{Show: Show{ID: "9"}}); r == nil || r.SeriesID != "9" {
		t.Errorf("by ID: %v", r)
	}
	if r := matchRelease(items, Season{Show: Show{ID: "1", Title: "重生·第3季"}}); r == nil || r.SeriesID != "7" {
		t.Errorf("by title: %v", r)
	}
	if r := matchRelease(items, Season{Show: Show{ID: "1"}}); r != nil {
		t.Errorf("no title matched %v", r)
	}
}

// An announced season says when it comes out; one out says it is new.
func TestAnnotate(t *testing.T) {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var s Season
	annotate(&s, &model.NewRelease{PublishAt: at, Heat: "159.7万人预约"}, time.UTC)
	if s.New || s.Note != "预计11月10日 12:00上线" || s.Heat != "159.7万人预约" {
		t.Errorf("announced: %+v", s)
	}
	s = Season{}
	annotate(&s, &model.NewRelease{PublishAt: at, Note: "预计11月10日上线"}, time.UTC)
	if s.Note != "预计11月10日上线" {
		t.Errorf("note: %+v", s)
	}
	s = Season{}
	annotate(&s, &model.NewRelease{Online: true, PublishAt: at}, time.UTC)
	if !s.New || s.Note != "11月10日上新" {
		t.Errorf("out: %+v", s)
	}
}
