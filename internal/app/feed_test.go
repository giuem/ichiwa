package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// A list ends where the source gives no cursor; a source that asks for
// the list again, as Hongguo does where its server says it ended, goes on
// while pages bring something new.
func TestFeedEndsWithoutCursor(t *testing.T) {
	f := &Feed{More: true, Items: make([]Card, 8)}
	if f.Step(feedPage{More: false}, true) {
		t.Fatalf("a page without a cursor didn't end the feed: %+v", f)
	}
	f = &Feed{More: true, Items: make([]Card, 8), stalls: 1}
	if !f.Step(feedPage{More: true, cursor: "again:c1"}, true) || f.cursor != "again:c1" || f.stalls != 0 {
		t.Fatalf("a page with something new: %+v", f)
	}
}

// A page of only repeats moves on from its cursor, and enough of them in a
// row end the feed, so it doesn't loop.
func TestFeedRepeatsMoveOn(t *testing.T) {
	f := &Feed{More: true, Items: make([]Card, 8), cursor: "c0"}
	if !f.Step(feedPage{More: true, cursor: "c1"}, false) || f.cursor != "c1" {
		t.Fatalf("a page of repeats: %+v", f)
	}
	f.Step(feedPage{More: true, cursor: "c2"}, false)
	if f.Step(feedPage{More: true, cursor: "c3"}, false) {
		t.Fatalf("%d pages of repeats didn't end the feed", maxStalls)
	}
}

// An end with nothing to show is believed at once: an empty search shows
// its message without waiting on tries.
func TestFeedEmptyEndsAtOnce(t *testing.T) {
	f := &Feed{More: true}
	if f.Step(feedPage{More: false}, false) || f.stalls != 0 {
		t.Fatalf("an empty end: %+v", f)
	}
}

// A collection's first items are kept on disk, named by source and
// collection; the next launch shows them, and the first page goes before
// them. A page with filters is not kept.
func TestFeedCache(t *testing.T) {
	cacheDir := t.TempDir()
	srcA := &sourcetest.Source{InfoData: source.Info{ID: "hongguo"}}
	srcB := &sourcetest.Source{InfoData: source.Info{ID: "other"}}

	app := &App{Src: srcA, CacheDir: cacheDir}
	cs := app.Col("channel:推荐")
	f := &cs.Results
	if f.stale || len(f.Items) != 0 {
		t.Fatalf("a first launch has nothing kept: %+v", f)
	}
	if want := feedCachePath(cacheDir, "hongguo", "channel:推荐"); f.cache != want || filepath.Dir(want) != filepath.Join(cacheDir, "feeds") {
		t.Fatalf("cache path = %q, want %q", f.cache, want)
	}
	cs.applyPage(feedPage{Page: &source.CollectionPage{Header: &source.Header{Title: "推荐", Description: "精选"}}})
	f.take(feedPage{Items: []Card{{ID: "a"}, {ID: "b"}, {ID: "c"}}})
	app.SaveFeeds(true)
	if f.changed {
		t.Fatal("saved, yet changed")
	}

	// Another source does not see hongguo's cached items.
	if of := (&App{Src: srcB, CacheDir: cacheDir}).Col("channel:推荐"); of.Results.stale || len(of.Results.Items) != 0 {
		t.Fatalf("feed cache leaked across sources: %+v", of.Results)
	}

	app = &App{Src: srcA, CacheDir: cacheDir}
	cs = app.Col("channel:推荐")
	f = &cs.Results
	if !f.stale || len(f.Items) != 3 || cs.Header == nil || cs.Header.Description != "精选" {
		t.Fatalf("not kept: %+v, header %+v", f, cs.Header)
	}
	app.SaveFeeds(true) // nothing new: the kept items stay
	fresh, added := f.take(feedPage{Items: []Card{{ID: "d"}, {ID: "b"}}})
	if !fresh || f.stale || len(added) != 4 {
		t.Fatalf("fresh = %v, added = %v", fresh, added)
	}
	var ids string
	for _, s := range f.Items {
		ids += s.ID
	}
	if ids != "dbac" {
		t.Fatalf("items = %s", ids)
	}

	// A page with filters is not kept.
	app = &App{Src: srcA, CacheDir: cacheDir}
	rank := app.Col("rank")
	rank.applyPage(feedPage{Page: &source.CollectionPage{Filters: []FilterRow{{Key: "board"}}}})
	rank.Results.take(feedPage{Items: []Card{{ID: "a"}}})
	app.SaveFeeds(true)
	if _, err := os.Stat(rank.Results.cache); !os.IsNotExist(err) {
		t.Fatalf("a page with filters was kept: %v", err)
	}
}

func TestFeedCacheMigratesLegacy(t *testing.T) {
	cacheDir := t.TempDir()
	legacyDir := filepath.Join(cacheDir, "feeds")
	if err := os.MkdirAll(filepath.Join(legacyDir, "hongguo"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, "recommend.json")
	legacyJSON := `[
		{"ID":"s1","Title":"旧缓存剧","Desc":"简介内容","Cover":"https://ex/1.jpg","Episodes":80,"Tag":"甜宠"}
	]`
	if err := os.WriteFile(legacyPath, []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	bySource := filepath.Join(legacyDir, "hongguo", "live.json")
	if err := os.WriteFile(bySource, []byte(`[{"kind":"media","id":"l1","title":"真人剧"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-hongguo source ignores the legacy file.
	otherSrc := &sourcetest.Source{InfoData: source.Info{ID: "other"}}
	if of := (&App{Src: otherSrc, CacheDir: cacheDir}).Col("channel:推荐"); of.Results.stale || len(of.Results.Items) != 0 {
		t.Fatalf("non-hongguo read legacy cache: %+v", of.Results)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("non-hongguo removed legacy cache: %v", err)
	}

	hgSrc := &sourcetest.Source{InfoData: source.Info{ID: "hongguo"}}
	app := &App{Src: hgSrc, CacheDir: cacheDir}
	f := &app.Col("channel:推荐").Results
	if !f.stale || len(f.Items) != 1 {
		t.Fatalf("migrated feed = %+v", f)
	}
	c := f.Items[0]
	if c.Kind != source.CardMedia || c.ID != "s1" || c.Title != "旧缓存剧" || c.Subtitle != "简介内容" ||
		c.Image != "https://ex/1.jpg" || !slices.Equal(c.Badges, []string{"全80集"}) || c.Note != "甜宠" {
		t.Fatalf("converted card = %+v", c)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy feed cache was not removed: %v", err)
	}
	if l := &app.Col("channel:真人剧").Results; len(l.Items) != 1 || l.Items[0].ID != "l1" {
		t.Fatalf("the file kept by source = %+v", l.Items)
	}
	if _, err := os.Stat(bySource); !os.IsNotExist(err) {
		t.Fatalf("the file kept by source was not removed: %v", err)
	}
	// A subsequent launch (even offline) reads from the new path.
	if f2 := &(&App{Src: hgSrc, CacheDir: cacheDir}).Col("channel:推荐").Results; !f2.stale || len(f2.Items) != 1 || f2.Items[0].Title != "旧缓存剧" {
		t.Fatalf("second launch feed = %+v", f2)
	}
}
