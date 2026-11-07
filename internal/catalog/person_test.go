package catalog

import (
	"reflect"
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
)

// A ranking's rows split into dramas and actors, and the rows the server
// inserts are left out.
func TestRankRows(t *testing.T) {
	shows, actors := rankRows([]model.RankItem{
		{SeriesID: "1", Title: "一江春水", Score: "2550万热度", EpisodeCount: 81},
		{SeriesID: "2", Title: "推广", Score: "平台推荐", Promoted: true},
		{SeriesID: "3", Title: "投票", Votes: 123456},
		{UserID: "u1", Actor: "李柯以", Score: "1060万人气", AvatarURL: "a.jpg", Details: []string{"54部作品"}},
		{Title: "neither"},
	})
	if len(shows) != 2 || shows[0].Tag != "2550万热度" || shows[0].Episodes != 81 || shows[1].Tag != "12.3万" {
		t.Errorf("shows = %+v", shows)
	}
	want := []Actor{{ID: "u1", Name: "李柯以", Avatar: "a.jpg", Score: "1060万人气", Details: []string{"54部作品"}}}
	if !reflect.DeepEqual(actors, want) {
		t.Errorf("actors = %+v", actors)
	}
}

// A work is tagged with its genre, not its episode count, which the badge
// already shows; with its plays when it has no genre.
func TestWorkShow(t *testing.T) {
	s := workShow(model.Work{SeriesID: "1", Title: "一江春水", EpisodeCount: 81, Tags: []string{"81集", "爱情"}})
	if s.ID != "1" || s.Episodes != 81 || s.Tag != "爱情" {
		t.Errorf("workShow = %+v", s)
	}
	if s := workShow(model.Work{SeriesID: "1", Tags: []string{"81集"}, Plays: 3517705}); s.Tag != "351.8万播放" {
		t.Errorf("tag = %q", s.Tag)
	}
}

// Rows left at "all" are not sent, and a choice has one name whatever the
// order of its rows.
func TestSelections(t *testing.T) {
	got := selections(map[string]string{"genre": "comic_series", "sort": "", "category_dim_theme": "cate_5876"})
	want := map[string][]string{"genre": {"comic_series"}, "category_dim_theme": {"cate_5876"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selections = %v", got)
	}
	a := FilterKey(map[string]string{"genre": "ai_series", "sort": "hot", "gender": ""})
	b := FilterKey(map[string]string{"sort": "hot", "genre": "ai_series"})
	if a != b || a != "genre=ai_series&sort=hot" {
		t.Errorf("FilterKey = %q, %q", a, b)
	}
	if FilterKey(nil) != "" {
		t.Error("no choice should have an empty key")
	}
}
