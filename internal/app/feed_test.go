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
	f := &Feed{more: true, items: make([]Card, 8)}
	if f.step(feedPage{more: false}, true) {
		t.Fatalf("a page without a cursor didn't end the feed: %+v", f)
	}
	f = &Feed{more: true, items: make([]Card, 8), stalls: 1}
	if !f.step(feedPage{more: true, cursor: "again:c1"}, true) || f.cursor != "again:c1" || f.stalls != 0 {
		t.Fatalf("a page with something new: %+v", f)
	}
}

// A page of only repeats moves on from its cursor, and enough of them in a
// row end the feed, so it doesn't loop.
func TestFeedRepeatsMoveOn(t *testing.T) {
	f := &Feed{more: true, items: make([]Card, 8), cursor: "c0"}
	if !f.step(feedPage{more: true, cursor: "c1"}, false) || f.cursor != "c1" {
		t.Fatalf("a page of repeats: %+v", f)
	}
	f.step(feedPage{more: true, cursor: "c2"}, false)
	if f.step(feedPage{more: true, cursor: "c3"}, false) {
		t.Fatalf("%d pages of repeats didn't end the feed", maxStalls)
	}
}

// An end with nothing to show is believed at once: an empty search shows
// its message without waiting on tries.
func TestFeedEmptyEndsAtOnce(t *testing.T) {
	f := &Feed{more: true}
	if f.step(feedPage{more: false}, false) || f.stalls != 0 {
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

	app := &App{src: srcA, cacheDir: cacheDir}
	cs := app.col("channel:推荐")
	f := &cs.results
	if f.stale || len(f.items) != 0 {
		t.Fatalf("a first launch has nothing kept: %+v", f)
	}
	if want := feedCachePath(cacheDir, "hongguo", "channel:推荐"); f.cache != want || filepath.Dir(want) != filepath.Join(cacheDir, "feeds") {
		t.Fatalf("cache path = %q, want %q", f.cache, want)
	}
	cs.applyPage(feedPage{page: &source.CollectionPage{Header: &source.Header{Title: "推荐", Description: "精选"}}})
	f.take(feedPage{items: []Card{{ID: "a"}, {ID: "b"}, {ID: "c"}}})
	app.saveFeeds(true)
	if f.changed {
		t.Fatal("saved, yet changed")
	}

	// Another source does not see hongguo's cached items.
	if of := (&App{src: srcB, cacheDir: cacheDir}).col("channel:推荐"); of.results.stale || len(of.results.items) != 0 {
		t.Fatalf("feed cache leaked across sources: %+v", of.results)
	}

	app = &App{src: srcA, cacheDir: cacheDir}
	cs = app.col("channel:推荐")
	f = &cs.results
	if !f.stale || len(f.items) != 3 || cs.header == nil || cs.header.Description != "精选" {
		t.Fatalf("not kept: %+v, header %+v", f, cs.header)
	}
	app.saveFeeds(true) // nothing new: the kept items stay
	fresh, added := f.take(feedPage{items: []Card{{ID: "d"}, {ID: "b"}}})
	if !fresh || f.stale || len(added) != 4 {
		t.Fatalf("fresh = %v, added = %v", fresh, added)
	}
	var ids string
	for _, s := range f.items {
		ids += s.ID
	}
	if ids != "dbac" {
		t.Fatalf("items = %s", ids)
	}

	// A page with filters is not kept.
	app = &App{src: srcA, cacheDir: cacheDir}
	rank := app.col("rank")
	rank.applyPage(feedPage{page: &source.CollectionPage{Filters: []FilterRow{{Key: "board"}}}})
	rank.results.take(feedPage{items: []Card{{ID: "a"}}})
	app.saveFeeds(true)
	if _, err := os.Stat(rank.results.cache); !os.IsNotExist(err) {
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
	if of := (&App{src: otherSrc, cacheDir: cacheDir}).col("channel:推荐"); of.results.stale || len(of.results.items) != 0 {
		t.Fatalf("non-hongguo read legacy cache: %+v", of.results)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("non-hongguo removed legacy cache: %v", err)
	}

	hgSrc := &sourcetest.Source{InfoData: source.Info{ID: "hongguo"}}
	app := &App{src: hgSrc, cacheDir: cacheDir}
	f := &app.col("channel:推荐").results
	if !f.stale || len(f.items) != 1 {
		t.Fatalf("migrated feed = %+v", f)
	}
	c := f.items[0]
	if c.Kind != source.CardMedia || c.ID != "s1" || c.Title != "旧缓存剧" || c.Subtitle != "简介内容" ||
		c.Image != "https://ex/1.jpg" || !slices.Equal(c.Badges, []string{"全80集"}) || c.Note != "甜宠" {
		t.Fatalf("converted card = %+v", c)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy feed cache was not removed: %v", err)
	}
	if l := &app.col("channel:真人剧").results; len(l.items) != 1 || l.items[0].ID != "l1" {
		t.Fatalf("the file kept by source = %+v", l.items)
	}
	if _, err := os.Stat(bySource); !os.IsNotExist(err) {
		t.Fatalf("the file kept by source was not removed: %v", err)
	}
	// A subsequent launch (even offline) reads from the new path.
	if f2 := &(&App{src: hgSrc, cacheDir: cacheDir}).col("channel:推荐").results; !f2.stale || len(f2.items) != 1 || f2.items[0].Title != "旧缓存剧" {
		t.Fatalf("second launch feed = %+v", f2)
	}
}
