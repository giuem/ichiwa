package ui

import (
	"path/filepath"
	"testing"
	"time"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// A plugin loaded in the developer mode shows its navigation and
// collections, filters, details and search, without touching the
// built-in source's pages.
func TestPluginSource(t *testing.T) {
	p, err := plugin.Open(filepath.Join("..", "plugin", "testdata", "example"), plugin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan func(), 16)
	builtin := &sourcetest.Source{InfoData: source.Info{ID: "hongguo", Name: "红果"}}
	app := &App{App: core.Headless(testLibrary(t), func(fn func()) { done <- fn }, builtin, p), pal: lightPalette(), posters: newPosters(t.TempDir(), func() {})}
	drain := func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}

	app.UseSource(p)
	if app.Page != "" {
		t.Fatalf("page = %q", app.Page)
	}
	drain() // navigation
	if app.Page != "home" || len(app.Nav) != 3 {
		t.Fatalf("page = %q, nav = %+v", app.Page, app.Nav)
	}

	tt := ui.NewTester(app.content, 1100, 800)
	// settle draws and runs what the loads bring until nothing comes.
	settle := func() {
		t.Helper()
		for range 50 {
			tt.Frame()
			select {
			case fn := <-done:
				fn()
			case <-time.After(300 * time.Millisecond):
				return
			}
		}
		t.Fatal("the page never settled")
	}
	settle()
	cs := app.Col("home")
	if len(cs.Results.Items) != 5 || cs.Results.More || cs.First == nil || len(cs.First.Sections) != 2 {
		t.Fatalf("home = %+v", cs)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "大兔子"); !ok {
		t.Fatal("the card 大兔子 is not shown")
	}

	// The filter page: a choice reloads it; a page whose list failed keeps
	// its rows and shows the error.
	app.Go("filter")
	settle()
	fs := app.Col("filter")
	if len(fs.Rows) != 2 || len(fs.Results.Items) == 0 {
		t.Fatalf("filter = %+v", fs)
	}
	if err := tt.Click("出错"); err != nil {
		t.Fatal(err)
	}
	settle()
	if fs.Results.Err != "列表暂时取不到，请重试" || len(fs.Rows) != 2 || fs.Chosen["genre"] != "broken" {
		t.Fatalf("broken filter: err %q rows %d chosen %v", fs.Results.Err, len(fs.Rows), fs.Chosen)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "重试"); !ok {
		t.Fatal("no retry for the failed list")
	}

	// Search.
	app.Go(core.PageSearch)
	app.RunSearch("辛特尔")
	drain()
	if len(app.Search.Results.Items) != 1 || app.Search.Results.Items[0].ID != "sintel" {
		t.Fatalf("search = %+v", app.Search.Results.Items)
	}

	// The details of a drama come from the plugin it belongs to.
	app.Open(p.Info().ID, app.Search.Results.Items[0], -1)
	drain()
	if app.Watch == nil || app.Watch.Detail == nil || len(app.Watch.Detail.Episodes) != 4 || !app.Watch.Has(source.CapDanmaku) {
		t.Fatalf("watch = %+v", app.Watch)
	}
	app.Watch = nil

	// A record of a source not loaded says so.
	app.Open("gone", Card{Kind: source.CardMedia, ID: "x", Title: "x"}, -1)
	if app.Watch.DetailErr == "" {
		t.Fatal("a record of a source not loaded opened")
	}
	app.Watch = nil

	// Back to the built-in source.
	app.UseSource(builtin)
	if app.Page != "" {
		t.Fatalf("back to the built-in: page %q", app.Page)
	}
	drain()
}
