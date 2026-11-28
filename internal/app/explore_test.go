package app

import (
	"context"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// Choosing a new option drops the results and loads under way; choosing the
// same option again changes nothing; applyPage takes the rows the source
// returns, with their choices.
func TestCollectionChoose(t *testing.T) {
	cs := newCol("filter", "筛选")
	cs.Results.setFetch(&sourcetest.Source{}, func(context.Context, source.Source, string) (feedPage, error) { return feedPage{}, nil })
	if !cs.Choose("theme", "t5") || cs.Chosen["theme"] != "t5" {
		t.Fatalf("choose theme: %+v", cs.Chosen)
	}
	if cs.Results.fetch != nil || cs.Results.gen != 1 {
		t.Error("a new choice should drop the old results and their fetch")
	}
	cs.Results.Loaded = true
	if cs.Choose("theme", "t5") || !cs.Results.Loaded {
		t.Error("choosing the same option again reset the results")
	}
	cs.Choose("genre", "comic")
	cs.applyPage(feedPage{Page: &source.CollectionPage{
		Filters: []FilterRow{
			{Key: "genre", Title: "体裁", Value: "comic", Options: []source.FilterOption{{Title: "全部体裁"}, {Value: "comic", Title: "漫剧"}}},
			{Key: "theme", Title: "题材", Options: []source.FilterOption{{Title: "全部题材"}}},
		},
	}})
	if len(cs.Chosen) != 1 || cs.Chosen["genre"] != "comic" {
		t.Errorf("applyPage: %v", cs.Chosen)
	}
	if !cs.Choose("genre", "") || len(cs.Chosen) != 0 {
		t.Errorf("all: %v", cs.Chosen)
	}
	cs.Choose("genre", "comic")
	cs.Clear()
	if len(cs.Chosen) != 0 {
		t.Errorf("clear: %v", cs.Chosen)
	}
}

func drainer(t *testing.T, done chan func()) func() {
	return func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}
}

// Refreshing a page drops its sections, canceling their calls, and leaving
// an opened collection cancels what it was still loading.
func TestRefreshAndLeaveCancelSections(t *testing.T) {
	canceled := make(chan string, 4)
	entered := make(chan struct{}, 4)
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a"},
		OnCollection: func(ctx context.Context, r source.CollectionRequest) (*source.CollectionPage, error) {
			if r.ID == "home" {
				return &source.CollectionPage{Sections: []source.Section{{Title: "分区", Collection: "sec"}}}, nil
			}
			entered <- struct{}{}
			<-ctx.Done()
			canceled <- r.ID
			return nil, ctx.Err()
		},
	}
	app, run := cancelApp(t, src)
	cs := app.Col("home")
	app.Ensure(cs)
	run(5 * time.Second)
	app.SectionFeed(cs.sections, src, "sec")
	waitEntered(t, entered)
	cs.Refresh()
	select {
	case id := <-canceled:
		if id != "sec" {
			t.Fatalf("canceled %s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refreshing left the section loading")
	}
	for run(300 * time.Millisecond) {
	}

	app.OpenCollection(Card{Kind: source.CardCollection, ID: "person:p", Title: "人"})
	app.Ensure(app.Stack[0])
	waitEntered(t, entered)
	app.CloseCollection()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("leaving the page left its list loading")
	}
}
