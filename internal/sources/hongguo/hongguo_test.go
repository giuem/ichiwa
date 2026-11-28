package hongguo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beilunyang/openhongguo/adapters/httptransport"
	"github.com/beilunyang/openhongguo/adapters/memstore"
	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"

	"github.com/giuem/ichiwa/internal/source"
)

const registerRespJSON = `{"new_user":1,"device_id_str":"7100000000000001","install_id_str":"7100000000000002"}`

func newTestSource(t *testing.T, handler http.Handler) *Source {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := sdk.NewClient(sdk.Options{
		Transport:          httptransport.New(),
		ProfileStore:       memstore.New(),
		APIOrigin:          srv.URL,
		RegisterOrigin:     srv.URL,
		DistributionOrigin: srv.URL,
		RetryAttempts:      1,
		DeviceResets:       sdk.Int(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Source{
		client: client,
		days:   map[string]calDay{},
		panels: map[string]panelCache{},
	}
}

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

	rawRows := []model.FilterRow{
		{Key: "genre", Name: "全部体裁", Options: []model.FilterOption{{Name: "真人剧", Value: "short_play"}, {Name: "漫剧", Value: "comic_series"}}},
		{Key: "theme", Name: "全部题材", Options: []model.FilterOption{{Name: "玄幻", Value: "xuanhuan"}}},
		{Key: "sort", Name: "全部推荐", Options: []model.FilterOption{{Name: "最高热度", Value: "hot_score"}}},
	}

	// Selecting within the same genre keeps prefixed selections and strips the prefix for SDK.
	eff, rows := buildFilterRows(rawRows, map[string]string{
		"genre": "comic_series",
		"theme": "comic_series:xuanhuan",
		"sort":  "comic_series:hot_score",
	})
	if !reflect.DeepEqual(eff, map[string]string{"genre": "comic_series", "theme": "xuanhuan", "sort": "hot_score"}) {
		t.Fatalf("same-genre effective = %v", eff)
	}
	if rows[0].Value != "comic_series" || rows[1].Value != "comic_series:xuanhuan" || rows[2].Value != "comic_series:hot_score" {
		t.Fatalf("same-genre rows = %+v", rows)
	}

	// Switching genre clears other rows even when the new genre offers the exact same raw value ("hot_score").
	eff, rows = buildFilterRows(rawRows, map[string]string{
		"genre": "short_play",
		"theme": "comic_series:xuanhuan",
		"sort":  "comic_series:hot_score",
	})
	if !reflect.DeepEqual(eff, map[string]string{"genre": "short_play"}) {
		t.Fatalf("switched-genre effective = %v", eff)
	}
	if rows[0].Value != "short_play" || rows[1].Value != "" || rows[2].Value != "" {
		t.Fatalf("switched-genre rows = %+v", rows)
	}

	// Switching back to "all genres" ("") also clears selections from a specific genre.
	eff, rows = buildFilterRows(rawRows, map[string]string{
		"sort": "short_play:hot_score",
	})
	if len(eff) != 0 || rows[2].Value != "" || rows[2].Options[1].Value != ":hot_score" {
		t.Fatalf("all-genre effective=%v rows=%+v", eff, rows)
	}
}

func TestFilterPaginationAndGenreSwitch(t *testing.T) {
	const panelJSON = `{
		"code": 0,
		"data": {
			"selector_rows": [
				{"type":"genre","row_name":"全部体裁","items":[{"show_name":"真人剧","selector_item_id":"short_play"},{"show_name":"漫剧","selector_item_id":"comic_series"}]},
				{"type":"category_dim_theme","row_name":"全部主题","items":[{"show_name":"悬疑","selector_item_id":"cate_165"}]},
				{"type":"sort","row_name":"全部推荐","items":[{"show_name":"最高热度","selector_item_id":"hot_score"}]}
			]
		}
	}`
	const page1JSON = `{
		"code": 0,
		"data": {
			"has_more": true,
			"next_offset": 18,
			"session_id": "s1",
			"video_data": [{"series_id":"7100000000000101","title":"漫剧一","episode_cnt":20}]
		}
	}`
	const page2JSON = `{
		"code": 0,
		"data": {
			"has_more": false,
			"next_offset": 36,
			"session_id": "s1",
			"video_data": [{"series_id":"7100000000000102","title":"漫剧二","episode_cnt":30}]
		}
	}`

	var contentReqs int
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/service/2/device_register/" {
			_, _ = w.Write([]byte(registerRespJSON))
			return
		}
		if r.URL.Path == "/reading/distribution/category/landpage/v1/" {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if body["req_type"] == "only_panel" {
				_, _ = w.Write([]byte(panelJSON))
				return
			}
			contentReqs++
			if contentReqs == 1 {
				_, _ = w.Write([]byte(page1JSON))
				return
			}
			_, _ = w.Write([]byte(page2JSON))
			return
		}
		http.NotFound(w, r)
	}))

	// Simulate switching from short_play (with theme+sort selected) to comic_series:
	// stale choices from short_play must be dropped on page 1, and page 2 with
	// the same stale map + cursor must succeed without SDK mismatch errors.
	staleFilters := map[string]string{
		"genre":              "comic_series",
		"category_dim_theme": "short_play:cate_165",
		"sort":               "short_play:hot_score",
	}
	p1, err := src.Collection(context.Background(), source.CollectionRequest{
		ID:      ColFilter,
		Filters: staleFilters,
	})
	if err != nil {
		t.Fatalf("page 1 failed: %v", err)
	}
	if len(p1.Items) != 1 || p1.Cursor == "" {
		t.Fatalf("page 1 = %+v", p1)
	}
	if len(p1.Filters) != 3 || p1.Filters[0].Value != "comic_series" || p1.Filters[1].Value != "" || p1.Filters[2].Value != "" {
		t.Fatalf("page 1 filters = %+v", p1.Filters)
	}

	p2, err := src.Collection(context.Background(), source.CollectionRequest{
		ID:      ColFilter,
		Filters: staleFilters,
		Cursor:  p1.Cursor,
	})
	if err != nil {
		t.Fatalf("page 2 failed: %v", err)
	}
	if len(p2.Items) != 1 || p2.Items[0].ID != "7100000000000102" || !strings.HasPrefix(p2.Cursor, again) {
		t.Fatalf("page 2 = %+v", p2)
	}
}

func TestFilterFirstLoadResultsFailureKeepsPanel(t *testing.T) {
	const panelJSON = `{
		"code": 0,
		"data": {
			"selector_rows": [
				{"type":"genre","row_name":"全部体裁","items":[{"show_name":"漫剧","selector_item_id":"comic_series"}]}
			]
		}
	}`
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/service/2/device_register/" {
			_, _ = w.Write([]byte(registerRespJSON))
			return
		}
		if r.URL.Path == "/reading/distribution/category/landpage/v1/" {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if body["req_type"] == "only_panel" {
				_, _ = w.Write([]byte(panelJSON))
				return
			}
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))

	page, err := src.Collection(context.Background(), source.CollectionRequest{ID: ColFilter})
	if err == nil {
		t.Fatal("expected error when ShortFilter fails")
	}
	if page == nil || len(page.Filters) != 1 || page.Filters[0].Key != "genre" {
		t.Fatalf("expected filter panel alongside error, got page=%+v", page)
	}
}

func TestSeasonsOf(t *testing.T) {
	got := seasonsOf([]model.Season{
		{SeriesID: "3", Title: "剧 第三季", CoverURL: "https://ex/3.jpg", Index: 3},
		{SeriesID: "1", Title: "剧 第一季", CoverURL: "https://ex/1.jpg", Index: 1, EpisodeCount: 80},
		{SeriesID: "x", Title: "番外"},
		{SeriesID: "1", Index: 1},
		{SeriesID: "", Index: 2},
		{SeriesID: "2", Title: "剧 第二季", CoverURL: "https://ex/2.jpg", Index: 2, EpisodeCount: 60},
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
	if got[0].Title != "剧 第一季" || got[0].Cover != "https://ex/1.jpg" || got[2].Title != "剧 第三季" || got[2].Cover != "https://ex/3.jpg" {
		t.Errorf("title/cover = %+v", got)
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
	s := seasonEntry{ID: "s2", Title: "某剧 第二季", Index: 2}
	annotate(&s, &model.NewRelease{PublishAt: at, Heat: "159.7万人预约", CoverURL: "https://ex/cal2.jpg"}, time.UTC)
	ps := toProtocolSeason(s, false)
	if ps.IsAvailable() || ps.Title != "第2季" || ps.Name != "某剧 第二季" || ps.Image != "https://ex/cal2.jpg" || ps.Note != "预计11月10日 12:00上线 · 159.7万人预约" {
		t.Errorf("announced: %+v", ps)
	}

	s = seasonEntry{ID: "s2", Title: "某剧 第二季", Cover: "https://ex/orig.jpg", Index: 2}
	annotate(&s, &model.NewRelease{PublishAt: at, Note: "预计11月10日上线", CoverURL: "https://ex/cal2.jpg"}, time.UTC)
	if ps := toProtocolSeason(s, false); ps.Note != "预计11月10日上线" || ps.Image != "https://ex/orig.jpg" {
		t.Errorf("note: %+v", ps)
	}

	s = seasonEntry{ID: "s2", Title: "某剧 第二季", Index: 2, Online: true}
	annotate(&s, &model.NewRelease{Online: true, PublishAt: at}, time.UTC)
	if ps := toProtocolSeason(s, true); !ps.IsAvailable() || !ps.Current || ps.Note != "11月10日上新" {
		t.Errorf("out: %+v", ps)
	}
}

func TestMediaCalendarTimeoutConcurrentAndCache(t *testing.T) {
	const seriesJSON = `{
		"code": 0,
		"data": {
			"video_data": {
				"series_id_str": "7100000000000101",
				"series_title": "测试剧 第一季",
				"series_cover": "https://media.invalid/s1.jpg",
				"episode_cnt": 1,
				"video_list": [{"vid":"7100000000000201","vid_index":1,"title":"第1集","duration":60,"series_id":"7100000000000101"}]
			}
		}
	}`
	const seasonsJSON = `{
		"code": 0,
		"data": [{
			"show_type": 547,
			"video_data": [
				{"series_id":"7100000000000101","title":"测试剧 第一季","cover":"https://media.invalid/s1.jpg","episode_cnt":60,"season_index":1},
				{"series_id":"7100000000000502","title":"测试剧 第二季","cover":"https://media.invalid/s2.jpg","episode_cnt":0,"season_index":2}
			]
		}]
	}`
	const todayJSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [],
			"has_more": false,
			"calendar_schema": {"date_list":["20261023","20261024","20261025"],"default_date":"20261023","current_date":"20261023"}
		}
	}`
	const futureEmptyJSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [],
			"has_more": false,
			"calendar_schema": {"date_list":["20261023","20261024","20261025"],"default_date":"20261023","current_date":"20261024"}
		}
	}`
	const futureMatchJSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [{
				"is_online": false,
				"expected_publish_time": 1793001600,
				"rec_tags": [{"content":"50万人预约","data_type":27}],
				"subscribe_data": {
					"series_id": "7100000000000502",
					"title": "测试剧 第二季",
					"cover": "https://media.invalid/s2-cal.jpg",
					"episode_cnt": 0
				}
			}],
			"has_more": false,
			"calendar_schema": {"date_list":["20261023","20261024","20261025"],"default_date":"20261023","current_date":"20261025"}
		}
	}`

	var (
		inflight    atomic.Int32
		maxInflight atomic.Int32
		bgDone      = make(chan struct{})
		futureDone  atomic.Int32
		once        sync.Once
	)
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/novel/player/video_detail/v1/":
			_, _ = w.Write([]byte(seriesJSON))
		case "/reading/bookapi/plan/v1/":
			_, _ = w.Write([]byte(seasonsJSON))
		case "/reading/user/subscribe/list/v1/":
			day := r.URL.Query().Get("target_date")
			if day == "" {
				_, _ = w.Write([]byte(todayJSON))
				return
			}
			cur := inflight.Add(1)
			for {
				prev := maxInflight.Load()
				if cur <= prev || maxInflight.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(120 * time.Millisecond)
			inflight.Add(-1)
			if day == "20261025" {
				_, _ = w.Write([]byte(futureMatchJSON))
			} else {
				_, _ = w.Write([]byte(futureEmptyJSON))
			}
			if futureDone.Add(1) == 2 {
				once.Do(func() { close(bgDone) })
			}
		default:
			http.NotFound(w, r)
		}
	}))
	src.calendarBudget = 40 * time.Millisecond

	start := time.Now()
	m1, err := src.Media(context.Background(), "7100000000000101")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("first Media failed: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("first Media took %v, expected <= 200ms with 40ms budget", elapsed)
	}
	if len(m1.Seasons) != 2 {
		t.Fatalf("first Media seasons = %+v, want 2", m1.Seasons)
	}
	if m1.Seasons[1].Name != "测试剧 第二季" || m1.Seasons[1].Image != "https://media.invalid/s2.jpg" {
		t.Fatalf("season[1] name/image = %+v", m1.Seasons[1])
	}
	if m1.Seasons[1].Note != "" {
		t.Fatalf("first Media should have timed out before calendar note, got %q", m1.Seasons[1].Note)
	}

	// Wait for background calendar reads to finish and verify they ran concurrently.
	select {
	case <-bgDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background calendar reads did not complete")
	}
	if maxInflight.Load() < 2 {
		t.Fatalf("max concurrent calendar day reads = %d, want >= 2", maxInflight.Load())
	}

	// Wait briefly for cache write after handler returned.
	deadline := time.Now().Add(time.Second)
	for {
		src.mu.Lock()
		cached := len(src.days) >= 3
		src.mu.Unlock()
		if cached || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second Media call hits cached calendar days and returns annotated season promptly.
	m2, err := src.Media(context.Background(), "7100000000000101")
	if err != nil {
		t.Fatalf("second Media failed: %v", err)
	}
	if len(m2.Seasons) != 2 || m2.Seasons[1].Note == "" {
		t.Fatalf("second Media seasons should be annotated from cache: %+v", m2.Seasons)
	}
}

func TestCalendarPartialDayNotCached(t *testing.T) {
	const page1JSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [{
				"is_online": true,
				"subscribe_data": {"series_id":"1","title":"剧一","episode_cnt":10}
			}],
			"next_offset": 2,
			"has_more": true,
			"session_id": "s1",
			"calendar_schema": {"date_list":["20261023"],"default_date":"20261023","current_date":"20261023"}
		}
	}`
	const page2JSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [{
				"is_online": true,
				"subscribe_data": {"series_id":"2","title":"剧二","episode_cnt":20}
			}],
			"has_more": false,
			"calendar_schema": {"date_list":["20261023"],"default_date":"20261023","current_date":"20261023"}
		}
	}`

	var failPage2 atomic.Bool
	failPage2.Store(true)
	var hits atomic.Int32

	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/service/2/device_register/" {
			_, _ = w.Write([]byte(registerRespJSON))
			return
		}
		if r.URL.Path == "/reading/user/subscribe/list/v1/" {
			hits.Add(1)
			if r.URL.Query().Get("offset") == "0" || r.URL.Query().Get("offset") == "" {
				_, _ = w.Write([]byte(page1JSON))
				return
			}
			if failPage2.Load() {
				http.Error(w, "halfway failure", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(page2JSON))
			return
		}
		http.NotFound(w, r)
	}))

	// First call: page 1 succeeds, page 2 fails -> must NOT be cached.
	d1, err := src.calendar(context.Background(), "20261023")
	if err != nil {
		t.Fatalf("partial day returned error: %v", err)
	}
	if len(d1.items) != 1 {
		t.Fatalf("partial day items = %d, want 1", len(d1.items))
	}
	src.mu.Lock()
	_, cached := src.days["20261023"]
	src.mu.Unlock()
	if cached {
		t.Fatal("half-read calendar day was cached")
	}

	// Second call with page 2 healthy: re-fetches both pages and caches the complete day.
	failPage2.Store(false)
	d2, err := src.calendar(context.Background(), "20261023")
	if err != nil {
		t.Fatalf("complete day failed: %v", err)
	}
	if len(d2.items) != 2 {
		t.Fatalf("complete day items = %d, want 2", len(d2.items))
	}
	beforeHits := hits.Load()
	d3, err := src.calendar(context.Background(), "20261023")
	if err != nil || len(d3.items) != 2 || hits.Load() != beforeHits {
		t.Fatalf("complete day should be served from cache: len=%d hits=%d->%d err=%v", len(d3.items), beforeHits, hits.Load(), err)
	}
}

func TestPersonPageFetchesProfileAndWorksTogether(t *testing.T) {
	const personJSON = `{
		"code": 0,
		"data": {
			"ugc_user": {
				"user_id": "7100000000001701",
				"base_info": {"user_id":"7100000000001701","user_name":"李柯以","user_avatar":"https://media.invalid/a.jpg","description":"简介"},
				"user_relation": {"fans_num":10000,"recv_digged_count":20000}
			}
		}
	}`
	const tabsJSON = `{
		"code": 0,
		"data": {"tabs":[{"name":"剧作","tab_type":8,"sub_tabs":[{"name":"全部","tab_type":100}]}]}
	}`
	const worksJSON = `{
		"code": 0,
		"data": {
			"common_list_info": {"cursor":"c2","has_more":true},
			"data_list": [{
				"data_type": 1,
				"video": {
					"video_id": "7100000000001801",
					"common": {"title":"代表作","cover":"https://media.invalid/w1.jpg"},
					"expand": {"episode_cnt":60}
				}
			}]
		}
	}`

	var failWorks, failProfile atomic.Bool
	failWorks.Store(true)
	var worksCalls atomic.Int32

	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/reading/user/profile/get/v1/":
			if failProfile.Load() {
				http.Error(w, "profile down", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(personJSON))
		case "/novel/commentapi/user/profile/7100000000001701/v1":
			worksCalls.Add(1)
			if failWorks.Load() {
				http.Error(w, "works down", http.StatusInternalServerError)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["tab_type"]; !ok {
				_, _ = w.Write([]byte(tabsJSON))
				return
			}
			_, _ = w.Write([]byte(worksJSON))
		default:
			http.NotFound(w, r)
		}
	}))

	const id = "person:7100000000001701"
	get := func(cursor string) (*source.CollectionPage, error) {
		return src.Collection(context.Background(), source.CollectionRequest{ID: id, Cursor: cursor})
	}

	// Without the works there is nothing to show: the page fails, retryably.
	if _, err := get(""); err == nil {
		t.Fatal("expected an error when the works fail")
	}

	// Profile and works together: the header and the works inline.
	failWorks.Store(false)
	p, err := get("")
	if err != nil {
		t.Fatalf("person page: %v", err)
	}
	if p.Header == nil || p.Header.Title != "李柯以" || len(p.Items) != 1 || p.Items[0].ID != "7100000000001801" || len(p.Sections) != 0 {
		t.Fatalf("person page = %+v", p)
	}

	// A profile that fails leaves the header out but still shows the works.
	failProfile.Store(true)
	p, err = get("")
	if err != nil {
		t.Fatalf("person page without profile: %v", err)
	}
	if p.Header != nil || len(p.Items) != 1 {
		t.Fatalf("person page without profile = %+v", p)
	}

	// Later pages are works only and do not read the profile again.
	if p.Cursor == "" {
		t.Fatal("first page has no cursor")
	}
	before := worksCalls.Load()
	wp, err := get(p.Cursor)
	if err != nil || wp.Header != nil || len(wp.Items) != 1 || worksCalls.Load() != before+1 {
		t.Fatalf("second page = %+v, %v (works calls %d → %d)", wp, err, before, worksCalls.Load())
	}
}

// The works do not wait long for a slow profile, nor at all when they fail.
func TestPersonWorksDoNotWaitForSlowProfile(t *testing.T) {
	const worksJSON = `{"code":0,"data":{"common_list_info":{"cursor":"c2","has_more":false},"data_list":[{"data_type":1,"video":{"video_id":"7100000000001801","common":{"title":"代表作"},"expand":{"episode_cnt":60}}}]}}`
	const tabsJSON = `{"code":0,"data":{"tabs":[{"name":"剧作","tab_type":8,"sub_tabs":[{"name":"全部","tab_type":100}]}]}}`
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var failWorks atomic.Bool
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/reading/user/profile/get/v1/":
			select {
			case <-release:
			case <-r.Context().Done():
			}
			http.Error(w, "late", http.StatusServiceUnavailable)
		case "/novel/commentapi/user/profile/7100000000001701/v1":
			if failWorks.Load() {
				http.Error(w, "works down", http.StatusInternalServerError)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["tab_type"]; !ok {
				_, _ = w.Write([]byte(tabsJSON))
				return
			}
			_, _ = w.Write([]byte(worksJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	src.profileWait = 50 * time.Millisecond
	get := func() (*source.CollectionPage, error, time.Duration) {
		start := time.Now()
		p, err := src.Collection(context.Background(), source.CollectionRequest{ID: "person:7100000000001701"})
		return p, err, time.Since(start)
	}
	p, err, took := get()
	if err != nil || p.Header != nil || len(p.Items) != 1 {
		t.Fatalf("page with a late profile = %+v, %v", p, err)
	}
	if took > time.Second {
		t.Fatalf("works waited %v for the profile", took)
	}
	failWorks.Store(true)
	if _, err, took = get(); err == nil || took > time.Second {
		t.Fatalf("failed works: err=%v after %v; want an error at once", err, took)
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

// A board chosen in another category is left out when the category
// changes, and a board alone brings its category.
func TestRankChoice(t *testing.T) {
	for _, c := range []struct {
		in             map[string]string
		content, board string
	}{
		{nil, "", ""},
		{map[string]string{"content": "a"}, "a", ""},
		{map[string]string{"content": "a", "board": "a:x"}, "a", "x"},
		{map[string]string{"content": "b", "board": "a:x"}, "b", ""},
		{map[string]string{"board": "a:x"}, "a", "x"},
		{map[string]string{"board": "x"}, "", ""},
	} {
		if content, board := rankChoice(c.in); content != c.content || board != c.board {
			t.Errorf("rankChoice(%v) = %q, %q; want %q, %q", c.in, content, board, c.content, c.board)
		}
	}
}

// The pages of the navigation say what they are on their first page only,
// and a list that ends early asks for itself again.
func TestNavHeadersAndAgain(t *testing.T) {
	if h := header(ColRecommend, ""); h == nil || h.Title != "推荐" {
		t.Errorf("header = %+v", h)
	}
	if h := header(ColRecommend, "c1"); h != nil {
		t.Errorf("a later page has a header %+v", h)
	}
	if got := nextCursor(false, "", "c1"); got != again+"c1" {
		t.Errorf("nextCursor = %q", got)
	}
	if got := nextCursor(true, "c2", again+"c1"); got != "c2" {
		t.Errorf("nextCursor = %q", got)
	}
	if got := resume(again + "c1"); got != "c1" {
		t.Errorf("resume = %q", got)
	}
}
