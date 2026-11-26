package hongguo

import (
	"context"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

const loadSeriesJSON = `{
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

const loadSeasonsJSON = `{
	"code": 0,
	"data": [{
		"show_type": 547,
		"video_data": [
			{"series_id":"7100000000000101","title":"测试剧 第一季","cover":"https://media.invalid/s1.jpg","episode_cnt":60,"season_index":1},
			{"series_id":"7100000000000502","title":"测试剧 第二季","cover":"https://media.invalid/s2.jpg","episode_cnt":0,"season_index":2}
		]
	}]
}`

// calendarJSON is a day of the release calendar listing dates, with
// nothing released on it.
func calendarJSON(current string, dates string) string {
	return `{"code":0,"data":{"subscribe_items":[],"has_more":false,"calendar_schema":{"date_list":[` + dates + `],"default_date":"20261023","current_date":"` + current + `"}}}`
}

// A season list that is slow to come must not hold up the details: Media
// returns without seasons once the budget is spent, and the list, read on
// in the background, is there the next time without being read again.
func TestMediaDoesNotWaitForSlowSeasonList(t *testing.T) {
	release := make(chan struct{})
	var seasonCalls atomic.Int32
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/novel/player/video_detail/v1/":
			_, _ = w.Write([]byte(loadSeriesJSON))
		case "/reading/bookapi/plan/v1/":
			seasonCalls.Add(1)
			<-release
			_, _ = w.Write([]byte(loadSeasonsJSON))
		case "/reading/user/subscribe/list/v1/":
			_, _ = w.Write([]byte(calendarJSON("20261023", `"20261023"`)))
		default:
			http.NotFound(w, r)
		}
	}))
	src.calendarBudget = 40 * time.Millisecond

	start := time.Now()
	m, err := src.Media(context.Background(), "7100000000000101")
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("Media took %v waiting for the season list", took)
	}
	if len(m.Seasons) != 0 || len(m.Episodes) != 1 {
		t.Fatalf("first Media = %d seasons, %d episodes; want 0 and 1", len(m.Seasons), len(m.Episodes))
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := src.cachedSeasons("7100000000000101"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the season list read in the background was not cached")
		}
		time.Sleep(5 * time.Millisecond)
	}
	m, err = src.Media(context.Background(), "7100000000000101")
	if err != nil {
		t.Fatalf("second Media: %v", err)
	}
	if len(m.Seasons) != 2 || m.Seasons[1].Name != "测试剧 第二季" {
		t.Fatalf("second Media seasons = %+v", m.Seasons)
	}
	if n := seasonCalls.Load(); n != 1 {
		t.Fatalf("season list read %d times, want 1", n)
	}
}

// Reading release notes for a season fans out over the calendar's days,
// but never more than calendarParallel at once.
func TestCalendarReadsAreBounded(t *testing.T) {
	dates := `"20261023","20261024","20261025","20261026","20261027","20261028","20261029","20261030","20261031","20261101"`
	var inflight, peak, days atomic.Int32
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/reading/user/subscribe/list/v1/":
			day := r.URL.Query().Get("target_date")
			if day == "" {
				_, _ = w.Write([]byte(calendarJSON("20261023", dates)))
				return
			}
			days.Add(1)
			n := inflight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(60 * time.Millisecond)
			inflight.Add(-1)
			_, _ = w.Write([]byte(calendarJSON(day, dates)))
		default:
			http.NotFound(w, r)
		}
	}))
	entries := []seasonEntry{
		{ID: "7100000000000101", Index: 1, Online: true},
		{ID: "7100000000000502", Index: 2, Online: false},
	}
	src.announce(context.Background(), entries)
	if days.Load() < calendarParallel+1 {
		t.Fatalf("read %d days; the test needs more than %d to mean anything", days.Load(), calendarParallel)
	}
	if p := peak.Load(); p > calendarParallel {
		t.Fatalf("%d calendar days read at once, want at most %d", p, calendarParallel)
	}
}

// Concurrent readers of the same calendar day share one request.
func TestCalendarDayReadOnce(t *testing.T) {
	var reads atomic.Int32
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/reading/user/subscribe/list/v1/":
			if r.URL.Query().Get("target_date") == "20261024" {
				reads.Add(1)
				time.Sleep(80 * time.Millisecond)
			}
			_, _ = w.Write([]byte(calendarJSON("20261024", `"20261023","20261024"`)))
		default:
			http.NotFound(w, r)
		}
	}))
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := src.calendar(context.Background(), "20261024"); err != nil {
				t.Errorf("calendar: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := reads.Load(); n != 1 {
		t.Fatalf("the day was read %d times, want 1", n)
	}
}

// A rank page after the first continues the ranking its cursor names,
// whatever the filters say: they may have moved on since (testdata from
// openhongguo, Apache-2.0).
func TestRankCursorIgnoresFilters(t *testing.T) {
	page, err := os.ReadFile("testdata/short-rank.json")
	if err != nil {
		t.Fatal(err)
	}
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/reading/bookapi/bookmall/cell/change/v1/":
			_, _ = w.Write(page)
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
		}
	}))
	first, err := src.Collection(context.Background(), source.CollectionRequest{ID: "rank"})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.Cursor == "" {
		t.Fatal("first page has no cursor")
	}
	_, err = src.Collection(context.Background(), source.CollectionRequest{
		ID:      "rank",
		Filters: map[string]string{"content": "some-other-content", "board": "some-other-board"},
		Cursor:  first.Cursor,
	})
	if err != nil {
		t.Fatalf("next page with filters that moved on: %v", err)
	}
}

// A calendar that fails leaves the release notes out this time only: the
// next opening reads it again, with the season list from the cache.
func TestFailedCalendarIsReadAgain(t *testing.T) {
	const dates = `"20261023","20261024","20261025"`
	const matchJSON = `{
		"code": 0,
		"data": {
			"subscribe_items": [{
				"is_online": false,
				"expected_publish_time": 1793001600,
				"subscribe_data": {"series_id": "7100000000000502", "title": "测试剧 第二季", "episode_cnt": 0}
			}],
			"has_more": false,
			"calendar_schema": {"date_list":[` + dates + `],"default_date":"20261023","current_date":"20261025"}
		}
	}`
	var failing atomic.Bool
	failing.Store(true)
	var seasonCalls atomic.Int32
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service/2/device_register/":
			_, _ = w.Write([]byte(registerRespJSON))
		case "/novel/player/video_detail/v1/":
			_, _ = w.Write([]byte(loadSeriesJSON))
		case "/reading/bookapi/plan/v1/":
			seasonCalls.Add(1)
			_, _ = w.Write([]byte(loadSeasonsJSON))
		case "/reading/user/subscribe/list/v1/":
			if failing.Load() {
				http.Error(w, "calendar down", http.StatusInternalServerError)
				return
			}
			day := r.URL.Query().Get("target_date")
			switch day {
			case "":
				_, _ = w.Write([]byte(calendarJSON("20261023", dates)))
			case "20261025":
				_, _ = w.Write([]byte(matchJSON))
			default:
				_, _ = w.Write([]byte(calendarJSON(day, dates)))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	src.calendarBudget = 2 * time.Second

	m, err := src.Media(context.Background(), "7100000000000101")
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	if len(m.Seasons) != 2 || m.Seasons[1].Note != "" {
		t.Fatalf("Media with the calendar down = %+v", m.Seasons)
	}

	failing.Store(false)
	m, err = src.Media(context.Background(), "7100000000000101")
	if err != nil {
		t.Fatalf("second Media: %v", err)
	}
	if len(m.Seasons) != 2 || m.Seasons[1].Note == "" {
		t.Fatalf("second Media did not read the recovered calendar: %+v", m.Seasons)
	}
	if n := seasonCalls.Load(); n != 1 {
		t.Fatalf("season list read %d times, want 1 (cached)", n)
	}
}
