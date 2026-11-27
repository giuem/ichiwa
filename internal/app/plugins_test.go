package app

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
	app := &App{
		src:     builtin,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		warmed:  true,
		post:    func(fn func()) { done <- fn },
	}
	app.sources.add(builtin)
	app.sources.add(p)
	drain := func() {
		t.Helper()
		select {
		case fn := <-done:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for async callback")
		}
	}

	app.useSource(p)
	if app.builtin() || app.page != "" {
		t.Fatalf("page = %q", app.page)
	}
	drain() // navigation
	if app.page != "home" || len(app.nav) != 3 {
		t.Fatalf("page = %q, nav = %+v", app.page, app.nav)
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
	cs := app.col("home")
	if len(cs.results.items) != 5 || cs.results.more || cs.first == nil || len(cs.first.Sections) != 2 {
		t.Fatalf("home = %+v", cs)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "大兔子"); !ok {
		t.Fatal("the card 大兔子 is not shown")
	}

	// The filter page: a choice reloads it; a page whose list failed keeps
	// its rows and shows the error.
	app.go2("filter")
	settle()
	fs := app.col("filter")
	if len(fs.rows) != 2 || len(fs.results.items) == 0 {
		t.Fatalf("filter = %+v", fs)
	}
	if err := tt.Click("出错"); err != nil {
		t.Fatal(err)
	}
	settle()
	if fs.results.err != "列表暂时取不到，请重试" || len(fs.rows) != 2 || fs.chosen["genre"] != "broken" {
		t.Fatalf("broken filter: err %q rows %d chosen %v", fs.results.err, len(fs.rows), fs.chosen)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "重试"); !ok {
		t.Fatal("no retry for the failed list")
	}

	// Search.
	app.go2(pageSearch)
	app.runSearch("辛特尔")
	drain()
	if len(app.search.results.items) != 1 || app.search.results.items[0].ID != "sintel" {
		t.Fatalf("search = %+v", app.search.results.items)
	}

	// The details of a drama come from the plugin it belongs to.
	app.open(p.Info().ID, app.search.results.items[0], -1)
	drain()
	if app.watch == nil || app.watch.detail == nil || len(app.watch.detail.Episodes) != 4 || !app.watch.has(source.CapDanmaku) {
		t.Fatalf("watch = %+v", app.watch)
	}
	app.watch = nil

	// A record of a source not loaded says so.
	app.open("gone", Card{Kind: source.CardMedia, ID: "x", Title: "x"}, -1)
	if app.watch.detailErr == "" {
		t.Fatal("a record of a source not loaded opened")
	}
	app.watch = nil

	// Back to the built-in source.
	app.useSource(builtin)
	if !app.builtin() || app.page != pageRecommend || len(app.cols) != 0 {
		t.Fatalf("back to the built-in: page %q", app.page)
	}
	drain()
}

// Images and media of a plugin go through its own policy; a built-in
// source's take the plain client; an unknown source reaches nothing.
func TestSourcePolicies(t *testing.T) {
	p, err := plugin.Open(filepath.Join("..", "plugin", "testdata", "example"), plugin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var r registry
	r.add(&sourcetest.Source{InfoData: source.Info{ID: "hongguo"}})
	r.add(p)
	if c, h := r.imageRoute("https://picsum.photos/seed/x/300/400"); c == nil || h["Referer"] == "" {
		t.Fatal("a plugin's image does not go through the plugin")
	}
	if c, _ := r.imageRoute("https://p3-hongguo.example/cover.jpg"); c != nil {
		t.Fatal("a built-in image went through a plugin")
	}
	if pol := r.mediaPolicy("example"); !pol.Sandboxed || !pol.Hosts.Allows("test-videos.co.uk") {
		t.Fatalf("plugin policy = %+v", pol)
	}
	if pol := r.mediaPolicy("hongguo"); pol.Sandboxed {
		t.Fatal("the built-in source is sandboxed")
	}
	if pol := r.mediaPolicy("gone"); !pol.Sandboxed || len(pol.Hosts) != 0 {
		t.Fatalf("unknown source policy = %+v", pol)
	}
}

// A flood of log lines redraws the log page a few times, not once a line.
func TestThrottle(t *testing.T) {
	var th throttle
	var runs atomic.Int32
	for range 200000 {
		th.poke(50*time.Millisecond, func() { runs.Add(1) })
	}
	time.Sleep(150 * time.Millisecond)
	if n := runs.Load(); n < 1 || n > 3 {
		t.Fatalf("ran %d times", n)
	}
	th.poke(time.Millisecond, func() { runs.Add(1) })
	time.Sleep(50 * time.Millisecond)
	if runs.Load() < 2 {
		t.Fatal("a poke after the first run did not run")
	}
}
