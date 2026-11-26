package library

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/giuem/ichiwa/internal/catalog"

	"github.com/egoist/mygo/plugins/sqlite"
)

// Following a drama and following it again leaves it unfollowed, and the
// latest followed comes first.
func TestFollow(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	defer l.Close()
	a, b, c := catalog.Show{ID: "a"}, catalog.Show{ID: "b"}, catalog.Show{ID: "c", Title: "丙"}
	l.ToggleFollow(a)
	l.ToggleFollow(b)
	if got := l.Followed(); len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("Followed = %v", got)
	}
	l.ToggleFollow(a)
	if l.Following("a") || len(l.Followed()) != 1 {
		t.Fatal("following again should stop following")
	}
	l.ToggleFollow(c)
	again := Load(dir)
	defer again.Close()
	if got := again.Followed(); len(got) != 2 || got[0] != c || got[1].ID != "b" {
		t.Fatalf("follows are not saved in order: %v", got)
	}
}

// Progress and settings are saved by Flush, and a forgotten drama is gone.
func TestProgress(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	l.Progress(catalog.Show{ID: "a", Title: "甲"}, 3, 42.5, math.NaN())
	l.Progress(catalog.Show{ID: "b"}, 0, 1, 60)
	l.Volume, l.Speed, l.Page, l.OnTop, l.Autoplay, l.Danmaku = 0.5, 1.5, "rank", true, false, false
	l.DanmakuOpacity, l.DanmakuSpeed, l.DanmakuArea, l.DanmakuSize = 0.4, 0, 3, 4
	l.MarkDirty()
	l.Close()

	l = Load(dir)
	r := l.History["a"]
	if r == nil || r.Show.Title != "甲" || r.Episode != 3 || r.Position != 42.5 || r.Duration != 0 {
		t.Fatalf("History[a] = %+v", r)
	}
	if l.Volume != 0.5 || l.Speed != 1.5 || l.Page != "rank" || !l.OnTop || l.Autoplay || l.Danmaku ||
		l.DanmakuOpacity != 0.4 || l.DanmakuSpeed != 0 || l.DanmakuArea != 3 || l.DanmakuSize != 4 {
		t.Fatalf("settings = %v %v %q %v", l.Volume, l.Speed, l.Page, l.OnTop)
	}
	l.Forget("a")
	l.Close()

	l = Load(dir)
	defer l.Close()
	if _, ok := l.History["a"]; ok || len(l.History) != 1 {
		t.Fatalf("History = %v", l.History)
	}
}

// A library.json from earlier versions is imported once, then set aside.
func TestImportJSON(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "history": {"a": {"show": {"ID": "a", "Title": "甲"}, "episode": 2, "position": 10, "duration": 80, "updated": "2026-11-07T23:53:00+08:00"}},
  "follows": {"a": {"ID": "a"}, "b": {"ID": "b"}},
  "followOrder": ["b", "a"],
  "volume": 0.3, "speed": 2, "page": "comic"
}`
	if err := os.WriteFile(filepath.Join(dir, "library.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	l := Load(dir)
	defer l.Close()
	if r := l.History["a"]; r == nil || r.Episode != 2 || r.Updated.IsZero() {
		t.Fatalf("History[a] = %+v", r)
	}
	if got := l.Followed(); len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("Followed = %v", got)
	}
	if l.Volume != 0.3 || l.Speed != 2 || l.Page != "comic" {
		t.Fatalf("settings = %v %v %q", l.Volume, l.Speed, l.Page)
	}
	if _, err := os.Stat(filepath.Join(dir, "library.json.bak")); err != nil {
		t.Fatal("library.json was not set aside")
	}
}

// Each episode keeps how far it got; one watched to its end stays watched
// when watched again, and plays again from its start.
func TestEpisodes(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	s := catalog.Show{ID: "a", Episodes: 3}
	l.Progress(s, 0, 0, 0)
	l.Progress(s, 0, 59, 60)
	l.Progress(s, 1, 30, 60)
	l.Progress(s, 0, 0, 0) // again, from the start
	l.Close()

	l = Load(dir)
	defer l.Close()
	if !l.Watched("a", 0) || l.Watched("a", 1) || l.Watched("a", 2) {
		t.Fatalf("Episodes = %+v", l.Episodes["a"])
	}
	if got := l.Episode("a", 1).Fraction(); got != 0.5 {
		t.Fatalf("Fraction = %v", got)
	}
	if l.EpisodeStart("a", 0) != 0 || l.EpisodeStart("a", 1) != 30 || l.EpisodeStart("a", 2) != 0 {
		t.Fatal("EpisodeStart")
	}
	if l.History["a"].Done() {
		t.Fatal("not done")
	}
	l.Progress(s, 2, 60, 60)
	if !l.History["a"].Done() {
		t.Fatal("done")
	}
	l.Forget("a")
	if l.Episode("a", 2) != nil {
		t.Fatal("Forget keeps episodes")
	}
}

// A library of version 1 counts the episodes before the one each drama got
// to as watched.
func TestMigrateEpisodes(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	l.Progress(catalog.Show{ID: "a"}, 2, 10, 60)
	l.Flush()
	l.write(sqlite.Statement{SQL: "DROP TABLE episodes"}, sqlite.Statement{SQL: "PRAGMA user_version = 1"})
	l.Close()

	l = Load(dir)
	defer l.Close()
	if !l.Watched("a", 0) || !l.Watched("a", 1) || l.Watched("a", 2) || l.Episode("a", 2) != nil {
		t.Fatalf("Episodes = %+v", l.Episodes["a"])
	}
}
