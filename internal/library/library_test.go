package library

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"github.com/giuem/ichiwa/internal/source"
)

func cardOf(id, title string) source.Card {
	return source.Card{Kind: source.CardMedia, ID: id, Title: title, Shape: source.ShapePortrait}
}

// Following a drama and following it again leaves it unfollowed, the latest
// followed comes first, and records are isolated by source.
func TestFollow(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	defer l.Close()
	a, b, c := cardOf("a", "甲"), cardOf("b", "乙"), cardOf("c", "丙")
	l.ToggleFollow("hongguo", a)
	l.ToggleFollow("hongguo", b)
	l.ToggleFollow("other", a) // same media ID in another source
	if got := l.Followed(); len(got) != 3 || got[0].Source != "other" || got[0].Card.ID != "a" {
		t.Fatalf("Followed = %+v", got)
	}
	l.ToggleFollow("hongguo", a)
	if l.Following(source.Ref{Source: "hongguo", ID: "a"}) || !l.Following(source.Ref{Source: "other", ID: "a"}) {
		t.Fatal("following toggle leaked across sources")
	}
	l.ToggleFollow("hongguo", c)
	again := Load(dir)
	defer again.Close()
	got := again.Followed()
	if len(got) != 3 || !reflect.DeepEqual(got[0].Card, c) || got[1].Source != "other" || got[2].Card.ID != "b" {
		t.Fatalf("follows are not saved in order: %+v", got)
	}
}

// Progress and settings are saved by Flush, and a forgotten drama is gone.
func TestProgress(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	refA := source.Ref{Source: "hongguo", ID: "a"}
	refOther := source.Ref{Source: "other", ID: "a"}
	l.Progress("hongguo", cardOf("a", "甲"), "ep3", 3, 20, 42.5, math.NaN())
	l.Progress("other", cardOf("a", "别源甲"), "ep0", 0, 10, 1, 60)
	l.Volume, l.Speed, l.Page, l.OnTop, l.Autoplay, l.Danmaku = 0.5, 1.5, "rank", true, false, false
	l.DanmakuOpacity, l.DanmakuSpeed, l.DanmakuArea, l.DanmakuSize = 0.4, 0, 3, 4
	l.Source, l.Off["hongguo"], l.Off["gone"] = "other", true, false
	l.MarkDirty()
	l.Close()

	l = Load(dir)
	r := l.History[refA]
	if r == nil || r.Card.Title != "甲" || r.EpisodeID != "ep3" || r.Episode != 3 || r.Episodes != 20 || r.Position != 42.5 || r.Duration != 0 {
		t.Fatalf("History[hongguo/a] = %+v", r)
	}
	if l.Volume != 0.5 || l.Speed != 1.5 || l.Page != "rank" || !l.OnTop || l.Autoplay || l.Danmaku ||
		l.DanmakuOpacity != 0.4 || l.DanmakuSpeed != 0 || l.DanmakuArea != 3 || l.DanmakuSize != 4 {
		t.Fatalf("settings = %v %v %q %v", l.Volume, l.Speed, l.Page, l.OnTop)
	}
	if l.Source != "other" || !l.Off["hongguo"] || len(l.Off) != 1 {
		t.Fatalf("sources: shown %q, off %v", l.Source, l.Off)
	}
	l.Forget(refA)
	l.Close()

	l = Load(dir)
	defer l.Close()
	if _, ok := l.History[refA]; ok || l.History[refOther] == nil {
		t.Fatalf("History = %+v", l.History)
	}
}

// A library.json from earlier versions is imported once under hongguo, then set aside.
func TestImportJSON(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "history": {"a": {"show": {"ID": "a", "Title": "甲", "Episodes": 50, "Cover": "https://ex/a.jpg", "Tag": "甜宠"}, "episode": 2, "position": 10, "duration": 80, "updated": "2026-11-07T23:53:00+08:00"}},
  "follows": {"a": {"ID": "a", "Title": "甲"}, "b": {"ID": "b", "Title": "乙"}},
  "followOrder": ["b", "a"],
  "volume": 0.3, "speed": 2, "page": "comic"
}`
	if err := os.WriteFile(filepath.Join(dir, "library.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	l := Load(dir)
	defer l.Close()
	refA := source.Ref{Source: LegacySource, ID: "a"}
	r := l.History[refA]
	if r == nil || r.Source != LegacySource || r.Card.Title != "甲" || r.Card.Image != "https://ex/a.jpg" || r.Episode != 2 || r.Episodes != 50 || r.Updated.IsZero() {
		t.Fatalf("History[a] = %+v", r)
	}
	if got := l.Followed(); len(got) != 2 || got[0].Source != LegacySource || got[0].Card.ID != "b" {
		t.Fatalf("Followed = %+v", got)
	}
	if l.Volume != 0.3 || l.Speed != 2 || l.Page != "comic" {
		t.Fatalf("settings = %v %v %q", l.Volume, l.Speed, l.Page)
	}
	if _, err := os.Stat(filepath.Join(dir, "library.json.bak")); err != nil {
		t.Fatal("library.json was not set aside")
	}
	// Episode indices 0 and 1 from history.episode=2 resolve to episode IDs.
	l.ResolveEpisodes(refA, []string{"e0", "e1", "e2"})
	if !l.Watched(refA, "e0") || !l.Watched(refA, "e1") || l.Watched(refA, "e2") || r.EpisodeID != "e2" {
		t.Fatalf("resolved = %+v, rec = %+v", l.Episodes[refA], r)
	}
}

// Each episode keeps how far it got by episode ID; one watched to its end stays
// watched when watched again, and plays again from its start.
func TestEpisodes(t *testing.T) {
	dir := t.TempDir()
	l := Load(dir)
	ref := source.Ref{Source: "hongguo", ID: "a"}
	c := cardOf("a", "甲")
	l.Progress("hongguo", c, "e0", 0, 3, 0, 0)
	l.Progress("hongguo", c, "e0", 0, 3, 59, 60)
	l.Progress("hongguo", c, "e1", 1, 3, 30, 60)
	l.Progress("hongguo", c, "e0", 0, 3, 0, 0) // again, from the start
	l.Close()

	l = Load(dir)
	defer l.Close()
	if !l.Watched(ref, "e0") || l.Watched(ref, "e1") || l.Watched(ref, "e2") {
		t.Fatalf("Episodes = %+v", l.Episodes[ref])
	}
	if got := l.Episode(ref, "e1").Fraction(); got != 0.5 {
		t.Fatalf("Fraction = %v", got)
	}
	if l.EpisodeStart(ref, "e0") != 0 || l.EpisodeStart(ref, "e1") != 30 || l.EpisodeStart(ref, "e2") != 0 {
		t.Fatal("EpisodeStart")
	}
	if l.History[ref].Done() {
		t.Fatal("not done")
	}
	l.Progress("hongguo", c, "e2", 2, 3, 60, 60)
	if !l.History[ref].Done() {
		t.Fatal("done")
	}
	l.Forget(ref)
	if l.Episode(ref, "e2") != nil {
		t.Fatal("Forget keeps episodes")
	}
}

// A schema 1 database migrates to schema 3 under source "hongguo", and its
// implicit watched episodes (0..episode-1) map to episode IDs when resolved.
func TestMigrateFromSchema1(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "library.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var stmts []sqlite.Statement
	for _, sql := range migrations[0] {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	stmts = append(stmts,
		sqlite.Statement{
			SQL:  "INSERT INTO history (show_id, show, episode, position, duration, updated) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{"s1", `{"ID":"s1","Title":"旧剧","Desc":"简介","Cover":"https://ex/1.jpg","Episodes":10,"Tag":"都市"}`, 2, 15.0, 60.0, now},
		},
		sqlite.Statement{
			SQL:  "INSERT INTO follows (show_id, show, followed) VALUES (?, ?, ?)",
			Args: []any{"s1", `{"ID":"s1","Title":"旧剧","Episodes":10}`, now},
		},
		sqlite.Statement{
			SQL:  "INSERT INTO settings (key, value) VALUES (?, ?)",
			Args: []any{"volume", "0.7"},
		},
	)
	if _, err := db.Transaction(ctx, stmts); err != nil {
		t.Fatal(err)
	}
	db.Close()

	l := Load(dir)
	ref := source.Ref{Source: LegacySource, ID: "s1"}
	r := l.History[ref]
	if r == nil || r.Source != LegacySource || r.Card.Title != "旧剧" || r.Card.Subtitle != "简介" ||
		r.Card.Image != "https://ex/1.jpg" || r.Card.Note != "都市" || len(r.Card.Badges) != 1 || r.Card.Badges[0] != "全10集" ||
		r.Episode != 2 || r.Episodes != 10 || r.EpisodeID != "" {
		t.Fatalf("migrated history = %+v", r)
	}
	if !l.Following(ref) || l.Volume != 0.7 {
		t.Fatalf("following=%v volume=%v", l.Following(ref), l.Volume)
	}

	// Opening the drama supplies episode IDs and converts legacy progress.
	l.ResolveEpisodes(ref, []string{"ep-0", "ep-1", "ep-2", "ep-3"})
	if !l.Watched(ref, "ep-0") || !l.Watched(ref, "ep-1") || l.Watched(ref, "ep-2") || l.Episode(ref, "ep-2") != nil {
		t.Fatalf("Episodes after resolve = %+v", l.Episodes[ref])
	}
	if r.EpisodeID != "ep-2" {
		t.Fatalf("EpisodeID = %q, want ep-2", r.EpisodeID)
	}
	l.Close()

	// Reopening persists the resolved episode IDs and has no leftover legacy rows.
	again := Load(dir)
	defer again.Close()
	if !again.Watched(ref, "ep-0") || !again.Watched(ref, "ep-1") || again.History[ref].EpisodeID != "ep-2" {
		t.Fatalf("after reload: episodes=%+v history=%+v", again.Episodes[ref], again.History[ref])
	}
	if len(again.legacyEpisodes[ref]) != 0 {
		t.Fatalf("legacyEpisodes not cleared: %+v", again.legacyEpisodes[ref])
	}
}

// A schema 2 database migrates to schema 3 under source "hongguo", preserving
// per-episode positions and finished flags until resolved to episode IDs.
func TestMigrateFromSchema2(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "library.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var stmts []sqlite.Statement
	for _, sql := range migrations[0] {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	for _, sql := range migrations[1] {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	stmts = append(stmts,
		sqlite.Statement{
			SQL:  "INSERT INTO history (show_id, show, episode, position, duration, updated) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{"s2", `{"ID":"s2","Title":"二版剧","Episodes":5}`, 1, 25.0, 100.0, now},
		},
		sqlite.Statement{
			SQL:  "INSERT INTO episodes (show_id, episode, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{"s2", 0, 100.0, 100.0, 1, now},
		},
		sqlite.Statement{
			SQL:  "INSERT INTO episodes (show_id, episode, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{"s2", 1, 25.0, 100.0, 0, now},
		},
		sqlite.Statement{
			SQL:  "INSERT INTO follows (show_id, show, followed) VALUES (?, ?, ?)",
			Args: []any{"s2", `{"ID":"s2","Title":"二版剧","Cover":"https://ex/2.jpg","Episodes":5}`, now},
		},
	)
	if _, err := db.Transaction(ctx, stmts); err != nil {
		t.Fatal(err)
	}
	db.Close()

	l := Load(dir)
	ref := source.Ref{Source: LegacySource, ID: "s2"}
	if l.History[ref] == nil || !l.Following(ref) {
		t.Fatalf("migrated v2: history=%+v follows=%+v", l.History, l.Follows)
	}
	l.ResolveEpisodes(ref, []string{"id0", "id1", "id2"})
	if !l.Watched(ref, "id0") || l.Watched(ref, "id1") || l.EpisodeStart(ref, "id1") != 25 || l.Episode(ref, "id1").Fraction() != 0.25 {
		t.Fatalf("resolved v2 episodes = %+v", l.Episodes[ref])
	}
	if l.History[ref].EpisodeID != "id1" {
		t.Fatalf("History.EpisodeID = %q", l.History[ref].EpisodeID)
	}
	l.Close()

	again := Load(dir)
	defer again.Close()
	if !again.Watched(ref, "id0") || again.EpisodeStart(ref, "id1") != 25 || len(again.legacyEpisodes[ref]) != 0 {
		t.Fatalf("after reload: episodes=%+v legacy=%+v", again.Episodes[ref], again.legacyEpisodes[ref])
	}
}

// ResolveEpisodes deletes only the legacy rows that successfully resolved to an
// episode ID, keeping out-of-range or empty-ID indices for later resolution.
func TestResolveEpisodesPartial(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "library.db"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var stmts []sqlite.Statement
	for _, sql := range migrations[0] {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	for _, sql := range migrations[1] {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	stmts = append(stmts, sqlite.Statement{
		SQL:  "INSERT INTO history (show_id, show, episode, position, duration, updated) VALUES (?, ?, ?, ?, ?, ?)",
		Args: []any{"s3", `{"ID":"s3","Title":"部分剧集","Episodes":10}`, 5, 10.0, 60.0, now},
	})
	for i := range 6 {
		stmts = append(stmts, sqlite.Statement{
			SQL:  "INSERT INTO episodes (show_id, episode, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{"s3", i, float64(10 + i), 60.0, boolInt(i < 4), now},
		})
	}
	if _, err := db.Transaction(ctx, stmts); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ref := source.Ref{Source: LegacySource, ID: "s3"}
	l := Load(dir)
	// Supply only 3 episodes, with an empty ID at index 1: only 0 and 2 resolve.
	l.ResolveEpisodes(ref, []string{"ep0", "", "ep2"})
	if !l.Watched(ref, "ep0") || !l.Watched(ref, "ep2") {
		t.Fatalf("resolved episodes = %+v", l.Episodes[ref])
	}
	if got := len(l.legacyEpisodes[ref]); got != 4 {
		t.Fatalf("remaining legacy count = %d, want 4 (%+v)", got, l.legacyEpisodes[ref])
	}
	l.Close()

	// Reopening preserves the 4 unresolved rows in SQLite.
	again := Load(dir)
	defer again.Close()
	if got := len(again.legacyEpisodes[ref]); got != 4 {
		t.Fatalf("after reload remaining legacy count = %d, want 4", got)
	}
	for _, idx := range []int{1, 3, 4, 5} {
		if again.legacyEpisodes[ref][idx] == nil {
			t.Fatalf("legacy index %d was lost after partial resolve", idx)
		}
	}
	// Now supply the full list and verify all remaining legacy rows resolve and clear.
	again.ResolveEpisodes(ref, []string{"ep0", "ep1", "ep2", "ep3", "ep4", "ep5"})
	if !again.Watched(ref, "ep1") || !again.Watched(ref, "ep3") || again.EpisodeStart(ref, "ep4") != 14 || again.EpisodeStart(ref, "ep5") != 15 {
		t.Fatalf("fully resolved episodes = %+v", again.Episodes[ref])
	}
	if again.History[ref].EpisodeID != "ep5" {
		t.Fatalf("History.EpisodeID = %q, want ep5", again.History[ref].EpisodeID)
	}
	if len(again.legacyEpisodes[ref]) != 0 {
		t.Fatalf("legacy rows left after full resolve: %+v", again.legacyEpisodes[ref])
	}
}
