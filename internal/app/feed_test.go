package app

import "testing"

// A source that ends a list early, as Hongguo's feeds do now and then, is
// asked again from where it was; the feed ends after maxStalls such pages.
func TestFeedRetriesEarlyEnd(t *testing.T) {
	f := &Feed{more: true, items: make([]Show, 8)}
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
	f := &Feed{more: true, items: make([]Show, 8), cursor: "c0"}
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
