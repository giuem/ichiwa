package hongguo

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"github.com/giuem/ichiwa/internal/source"
)

func TestWrapErr(t *testing.T) {
	err := wrapErr(fmt.Errorf("resolve: %w", sdk.ErrAnonymousLimited))
	if got := source.Message(err); got != "服务端暂时限制了匿名播放，请稍后再试" {
		t.Fatalf("text = %q", got)
	}
	if !source.Is(err, source.ErrUnavailable) || source.RetryAfter(err) != source.DefaultRetryAfter {
		t.Fatalf("anonymous limited: code=%q retry=%v", source.CodeOf(err), source.RetryAfter(err))
	}
	if err := wrapErr(sdk.ErrTransport); !source.Is(err, source.ErrNetwork) || source.RetryAfter(err) != 0 {
		t.Fatalf("transport: code=%q retry=%v", source.CodeOf(err), source.RetryAfter(err))
	}
	if err := wrapErr(sdk.ErrAuthenticationRequired); !source.Is(err, source.ErrUnavailable) || source.RetryAfter(err) != 0 {
		t.Fatalf("auth required: code=%q retry=%v", source.CodeOf(err), source.RetryAfter(err))
	}
	if err := wrapErr(sdk.ErrRateLimited); !source.Is(err, source.ErrRateLimited) || source.RetryAfter(err) != source.DefaultRetryAfter {
		t.Fatalf("rate limited: code=%q retry=%v", source.CodeOf(err), source.RetryAfter(err))
	}
}

func TestRankCards(t *testing.T) {
	cards := rankCards([]model.RankItem{
		{SeriesID: "1", Title: "一江春水", Score: "2550万热度", EpisodeCount: 81},
		{SeriesID: "2", Title: "推广", Score: "平台推荐", Promoted: true},
		{SeriesID: "3", Title: "投票", Votes: 123456},
		{UserID: "u1", Actor: "李柯以", Score: "1060万人气", AvatarURL: "a.jpg", Details: []string{"54部作品", "双周追剧"}},
		{Title: "neither"},
	})
	if len(cards) != 3 {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[0].Kind != source.CardMedia || cards[0].Note != "2550万热度" || !slices.Equal(cards[0].Badges, []string{"全81集"}) {
		t.Errorf("card[0] = %+v", cards[0])
	}
	if cards[1].Note != "12.3万" {
		t.Errorf("card[1] = %+v", cards[1])
	}
	wantActor := source.Card{
		Kind:     source.CardCollection,
		ID:       "person:u1",
		Title:    "李柯以",
		Subtitle: "54部作品 · 双周追剧",
		Image:    "a.jpg",
		Shape:    source.ShapeCircle,
		Note:     "1060万人气",
	}
	if !reflect.DeepEqual(cards[2], wantActor) {
		t.Errorf("card[2] = %+v, want %+v", cards[2], wantActor)
	}
}

func TestCardOfWork(t *testing.T) {
	c := cardOfWork(model.Work{SeriesID: "1", Title: "一江春水", EpisodeCount: 81, Tags: []string{"81集", "爱情"}})
	if c.ID != "1" || !slices.Equal(c.Badges, []string{"全81集"}) || c.Note != "爱情" {
		t.Errorf("cardOfWork = %+v", c)
	}
	if c := cardOfWork(model.Work{SeriesID: "1", Tags: []string{"81集"}, Plays: 3517705}); c.Note != "351.8万播放" {
		t.Errorf("note = %q", c.Note)
	}
}

func TestSelectionsAndBuildFilterRows(t *testing.T) {
	got := selections(map[string]string{"genre": "comic_series", "sort": "", "category_dim_theme": "cate_5876"})
	want := map[string][]string{"genre": {"comic_series"}, "category_dim_theme": {"cate_5876"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selections = %v", got)
	}

	eff, rows := buildFilterRows([]model.FilterRow{
		{Key: "genre", Name: "全部体裁", Options: []model.FilterOption{{Name: "漫剧", Value: "comic_series"}}},
		{Key: "theme", Name: "全部题材", Options: []model.FilterOption{{Name: "玄幻", Value: "xuanhuan"}}},
	}, map[string]string{"genre": "comic_series", "theme": "stale_theme"})
	if !reflect.DeepEqual(eff, map[string]string{"genre": "comic_series"}) {
		t.Fatalf("effective = %v", eff)
	}
	if len(rows) != 2 || rows[0].Value != "comic_series" || rows[1].Value != "" || rows[1].Options[0].Title != "全部题材" {
		t.Fatalf("rows = %+v", rows)
	}
}

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

func TestMatchRelease(t *testing.T) {
	items := []model.NewRelease{{SeriesID: "9", Title: "别的剧"}, {SeriesID: "7", Title: "重生 第3季"}}
	if r := matchRelease(items, seasonEntry{ID: "9"}); r == nil || r.SeriesID != "9" {
		t.Errorf("by ID: %v", r)
	}
	if r := matchRelease(items, seasonEntry{ID: "1", Title: "重生·第3季"}); r == nil || r.SeriesID != "7" {
		t.Errorf("by title: %v", r)
	}
	if r := matchRelease(items, seasonEntry{ID: "1"}); r != nil {
		t.Errorf("no title matched %v", r)
	}
}

func TestAnnotateAndProtocolSeason(t *testing.T) {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	s := seasonEntry{ID: "s2", Index: 2}
	annotate(&s, &model.NewRelease{PublishAt: at, Heat: "159.7万人预约"}, time.UTC)
	ps := toProtocolSeason(s, false)
	if ps.IsAvailable() || ps.Title != "第2季" || ps.Note != "预计11月10日 12:00上线 · 159.7万人预约" {
		t.Errorf("announced: %+v", ps)
	}

	s = seasonEntry{ID: "s2", Index: 2}
	annotate(&s, &model.NewRelease{PublishAt: at, Note: "预计11月10日上线"}, time.UTC)
	if ps := toProtocolSeason(s, false); ps.Note != "预计11月10日上线" {
		t.Errorf("note: %+v", ps)
	}

	s = seasonEntry{ID: "s2", Index: 2, Online: true}
	annotate(&s, &model.NewRelease{Online: true, PublishAt: at}, time.UTC)
	if ps := toProtocolSeason(s, true); !ps.IsAvailable() || !ps.Current || ps.Note != "11月10日上新" {
		t.Errorf("out: %+v", ps)
	}
}

func TestEmojify(t *testing.T) {
	for in, want := range map[string]string{
		"哈哈[笑哭][笑哭]": "哈哈😂😂",
		"[吃瓜]看戏":     "🍉看戏",
		"[不认识]的代码":   "[不认识]的代码",
		"[[赞]":       "[👍",
		"没有括号":       "没有括号",
		"只有一半[笑":     "只有一半[笑",
		"a]b[爽]c":    "a]b😎c",
	} {
		if got := emojify(in); got != want {
			t.Errorf("emojify(%q) = %q, want %q", in, got, want)
		}
	}
}
