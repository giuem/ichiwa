package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// A source that ends a list early, as Hongguo's feeds do now and then, is
// asked again from where it was; the feed ends after maxStalls such pages.
func TestFeedRetriesEarlyEnd(t *testing.T) {
	f := &Feed{more: true, items: make([]Card, 8)}
	// A first page that shows a screen and gives no way on.
	if !f.step(feedPage{more: false}, true) || f.cursor != "" || f.stalls != 1 {
		t.Fatalf("an early end ended the feed: %+v", f)
	}
	// Asked again, it goes on: the stall is forgotten.
	if !f.step(feedPage{more: true, cursor: "c1"}, true) || f.cursor != "c1" || f.stalls != 0 {
		t.Fatalf("a page with a way on: %+v", f)
	}
	// A real end ends it after maxStalls tries, still from the last cursor.
	for i := 1; i < maxStalls; i++ {
		if !f.step(feedPage{more: false}, false) {
			t.Fatalf("ended after %d tries", i)
		}
	}
	if f.step(feedPage{more: false}, false) || f.cursor != "c1" {
		t.Fatalf("didn't end after %d tries: %+v", maxStalls, f)
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

// A feed's first items are kept on disk partitioned by source ID; the next
// launch shows them, and the first page goes before them.
func TestFeedCache(t *testing.T) {
	cacheDir := t.TempDir()
	srcA := &sourcetest.Source{InfoData: source.Info{ID: "hongguo"}}
	srcB := &sourcetest.Source{InfoData: source.Info{ID: "other"}}

	app := &App{src: srcA, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	f := app.feed(pageRecommend)
	if f.stale || len(f.items) != 0 {
		t.Fatalf("a first launch has nothing kept: %+v", f)
	}
	wantPath := filepath.Join(cacheDir, "feeds", "hongguo", "recommend.json")
	if f.cache != wantPath {
		t.Fatalf("cache path = %q, want %q", f.cache, wantPath)
	}
	f.take(feedPage{items: []Card{{ID: "a"}, {ID: "b"}, {ID: "c"}}})
	app.saveFeeds(true)
	if f.changed {
		t.Fatal("saved, yet changed")
	}

	// Another source does not see hongguo's cached items.
	otherApp := &App{src: srcB, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	if of := otherApp.feed(pageRecommend); of.stale || len(of.items) != 0 {
		t.Fatalf("feed cache leaked across sources: %+v", of)
	}

	app = &App{src: srcA, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	f = app.feed(pageRecommend)
	if !f.stale || len(f.items) != 3 {
		t.Fatalf("not kept: %+v", f)
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
	if f := (&App{src: srcA, feeds: map[string]*Feed{}, cacheDir: cacheDir}).feed(pageRank); f.cache != "" {
		t.Fatal("only the channels are kept")
	}
}

func TestFeedCacheMigratesLegacy(t *testing.T) {
	cacheDir := t.TempDir()
	legacyDir := filepath.Join(cacheDir, "feeds")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, "recommend.json")
	legacyJSON := `[
		{"ID":"s1","Title":"旧缓存剧","Desc":"简介内容","Cover":"https://ex/1.jpg","Episodes":80,"Tag":"甜宠"}
	]`
	if err := os.WriteFile(legacyPath, []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-hongguo source ignores the legacy file.
	otherSrc := &sourcetest.Source{InfoData: source.Info{ID: "other"}}
	otherApp := &App{src: otherSrc, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	if of := otherApp.feed(pageRecommend); of.stale || len(of.items) != 0 {
		t.Fatalf("non-hongguo read legacy cache: %+v", of)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("non-hongguo removed legacy cache: %v", err)
	}

	// Hongguo reads, converts fields, deletes the old file, and saves the new path.
	hgSrc := &sourcetest.Source{InfoData: source.Info{ID: "hongguo"}}
	app := &App{src: hgSrc, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	f := app.feed(pageRecommend)
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
	// A subsequent launch (even offline) reads from the new partitioned path.
	app2 := &App{src: hgSrc, feeds: map[string]*Feed{}, cacheDir: cacheDir}
	if f2 := app2.feed(pageRecommend); !f2.stale || len(f2.items) != 1 || f2.items[0].Title != "旧缓存剧" {
		t.Fatalf("second launch feed = %+v", f2)
	}
}
