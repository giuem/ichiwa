package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

// joinNonEmpty joins what there is of parts, as one line to read.
func joinNonEmpty(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "，")
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func (app *App) NavTitle(id string) string {
	for _, it := range app.Nav {
		if it.ID == id {
			return it.Title
		}
	}
	return strings.TrimPrefix(id, "channel:")
}

type searchState struct {
	Query   string
	Shown   string // the query the results are for
	Results Feed
	Hot     []Section
	HotErr  string
	hotBusy bool
	// hotFeeds are the sections of the search home that name their
	// collection, by its ID.
	HotFeeds map[string]*Feed
	// suggests are the words the source suggests for suggestFor, the
	// query typed; suggestGen counts the queries asked about.
	Suggests   []string
	SuggestFor string
	suggestGen int
	Focus      bool
	Told       string // the query screen readers were told the results of
}

// runSearch shows the results of q, or the search home when q is empty.
func (app *App) RunSearch(q string) {
	s := app.Search
	q = strings.TrimSpace(q)
	f := &s.Results
	f.reset()
	s.Shown, s.Told = q, ""
	s.Suggests, s.SuggestFor = nil, ""
	s.suggestGen++
	if q == "" {
		f.fetch = nil
		return
	}
	f.setFetch(app.Src, func(ctx context.Context, src source.Source, cursor string) (feedPage, error) {
		return pageResult(src.Search(ctx, source.SearchRequest{Query: q, Cursor: cursor}))
	})
	app.LoadMore(f)
}

// suggestWait is how long typing rests before the source is asked for
// words to suggest.
const suggestWait = 250 * time.Millisecond

// typed takes in the query as it is typed: an empty one goes back to the
// search home, another asks the source for words to suggest.
func (app *App) Typed(q string) {
	s := app.Search
	q = strings.TrimSpace(q)
	if q == "" {
		app.RunSearch("")
		return
	}
	if !app.HasCap(source.CapSearchSuggest) || q == s.Shown {
		s.Suggests, s.SuggestFor = nil, ""
		return
	}
	s.suggestGen++
	gen, src, ctx := s.suggestGen, app.Src, app.pageContext()
	async(app, func() ([]string, error) {
		select {
		case <-time.After(suggestWait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return src.Suggest(ctx, q)
	}, func(words []string, err error) {
		if s.suggestGen != gen || ctx.Err() != nil || err != nil {
			return
		}
		s.Suggests, s.SuggestFor = words, q
	})
}

// loadHot loads the search home sections, once.
func (app *App) LoadHot() {
	if !app.HasCap(source.CapSearchHome) {
		return
	}
	s := app.Search
	if s.Hot != nil || s.hotBusy || s.HotErr != "" {
		return
	}
	s.hotBusy = true
	src := app.Src
	ctx := app.srcContext()
	async(app, func() (*source.CollectionPage, error) {
		return src.SearchHome(ctx)
	}, func(p *source.CollectionPage, err error) {
		s.hotBusy = false // s is kept with its source when it is left
		if ctx.Err() != nil {
			return // canceled as the source was left: loads again when shown
		}
		if err != nil {
			s.HotErr = errText(err)
			return
		}
		if p != nil && p.Sections != nil {
			s.Hot = p.Sections
		} else {
			s.Hot = []Section{}
		}
	})
}

// HistoryShown is the records the history filter f shows, latest first.
func HistoryShown(recent []*Record, f int) []*Record {
	if f == 0 {
		return recent
	}
	out := make([]*Record, 0, len(recent))
	for _, r := range recent {
		if r.Done() == (f == 2) {
			out = append(out, r)
		}
	}
	return out
}

func (app *App) ToggleFollow(sourceID string, s Card) {
	app.Lib.ToggleFollow(sourceID, s)
	if app.Lib.Following(source.Ref{Source: sourceID, ID: s.ID}) {
		app.Toast = "已加入追剧"
	} else {
		app.Toast = "已取消追剧"
	}
}

// ShowSearch is the search page shown: a search canceled as the page was
// left searches again, and the search home loads.
func (app *App) ShowSearch() {
	app.LoadHot()
	if r := &app.Search.Results; r.Src != nil && !r.Loaded && !r.Loading && r.Err == "" {
		app.LoadMore(r)
	}
}
