package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/netguard"
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
	if c, h := r.imageRoute("https://p3-hongguo.example/cover.jpg"); c == nil || c.Transport == p.ImageClient().Transport || h != nil {
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

// An image not on a loaded plugin's hosts, as one of a record whose
// plugin is gone, never reaches this machine or its network, by name or
// by redirect.
func TestImageOfNoPluginIsGuarded(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the inside server got %s", r.URL)
	}))
	defer inside.Close()
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inside.URL+"/router/reboot?now=1", http.StatusFound)
	}))
	defer outside.Close()
	outsideAddr := netip.MustParseAddrPort(outside.Listener.Addr().String())
	_, insidePort, _ := net.SplitHostPort(inside.Listener.Addr().String())
	_, outsidePort, _ := net.SplitHostPort(outside.Listener.Addr().String())
	r := &registry{guard: &netguard.Guard{
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Exempt: func(ap netip.AddrPort) bool { return ap == outsideAddr },
	}}
	for _, u := range []string{
		"http://img.attacker.example:" + outsidePort + "/c.jpg", // redirects inside
		"http://nas.attacker.example:" + insidePort + "/c.jpg",  // resolves inside
		inside.URL + "/c.jpg",
	} {
		c, _ := r.imageRoute(u)
		resp, err := c.Get(u)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s fetched: %d", u, resp.StatusCode)
		} else if !errors.Is(err, netguard.ErrDenied) {
			t.Errorf("%s: %v", u, err)
		}
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

const devManifest = `{"id":"hongguo","name":"开发中的红果","kind":"source","version":"0.0.1","apiVersion":1,"main":"index.js","permissions":{"network":[]}}`

func devJS(title string) string {
	return `export function navigation() { return { items: [{ id: "home", title: "` + title + `" }] }; }
export function collection() { return { items: [] }; }
export function media() { return {}; }
export function play() { return {}; }
`
}

func writeDevDir(t *testing.T, js string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(devManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func devApp(t *testing.T) (*App, source.Source, chan func()) {
	t.Helper()
	done := make(chan func(), 64)
	builtin := &sourcetest.Source{InfoData: source.Info{ID: "hongguo", Name: "红果"}}
	app := &App{
		src:     builtin,
		feeds:   map[string]*Feed{},
		pal:     lightPalette(),
		posters: newPosters(t.TempDir(), func() {}),
		lib:     testLibrary(t),
		dataDir: t.TempDir(),
		post:    func(fn func()) { done <- fn },
	}
	app.sources.add(builtin)
	t.Cleanup(app.unloadDev)
	return app, builtin, done
}

// A plugin in the developer mode with the id of a built-in source stands
// in for it while loaded, and the built-in one comes back after.
func TestDevPluginStandsInForBuiltin(t *testing.T) {
	app, builtin, _ := devApp(t)
	dir := writeDevDir(t, devJS("一"))
	if err := app.loadDev(dir); err != nil {
		t.Fatal(err)
	}
	p := app.dev.plugin
	if app.src != source.Source(p) || app.sources.get("hongguo") != source.Source(p) {
		t.Fatalf("shown %v, registered %v", app.src, app.sources.get("hongguo"))
	}
	if all := app.sources.all(); len(all) != 1 || all[0] != source.Source(p) {
		t.Fatalf("sources = %v", all)
	}
	// What is shown is saved by whether it is the plugin.
	if s := app.loadDevSettings(); s.Dir != dir || s.Current != "hongguo" {
		t.Fatalf("saved %+v", s)
	}
	if pol := app.sources.mediaPolicy("hongguo"); !pol.Sandboxed {
		t.Fatal("the plugin's media are not sandboxed")
	}

	app.unloadDev()
	app.saveDevSettings()
	if app.src != builtin || app.sources.get("hongguo") != builtin {
		t.Fatalf("after unloading: shown %v, registered %v", app.src, app.sources.get("hongguo"))
	}
	if all := app.sources.all(); len(all) != 1 || all[0] != builtin {
		t.Fatalf("sources = %v", all)
	}
	if s := app.loadDevSettings(); s.Current != "" {
		t.Fatalf("saved %+v", s)
	}
	if pol := app.sources.mediaPolicy("hongguo"); pol.Sandboxed {
		t.Fatal("the built-in source's media are sandboxed")
	}

	// Loaded while another source is shown, it does not take over.
	other := &sourcetest.Source{InfoData: source.Info{ID: "other", Name: "别的"}}
	app.sources.add(other)
	app.useSource(other)
	if err := app.loadDev(dir); err != nil {
		t.Fatal(err)
	}
	if app.src != source.Source(other) || len(app.sources.all()) != 2 {
		t.Fatalf("shown %v, sources %v", app.src, app.sources.all())
	}
	if s := app.loadDevSettings(); s.Current != "" {
		t.Fatalf("saved %+v", s)
	}
}

// The menu's reload keeps the plugin and its code when the folder is
// broken, and a folder that did not load loads once it is fixed.
func TestDevReloadKeepsLastGoodCode(t *testing.T) {
	app, _, done := devApp(t)
	dir := writeDevDir(t, devJS("一"))
	if err := app.loadDev(dir); err != nil {
		t.Fatal(err)
	}
	p := app.dev.plugin
	write := func(js string) {
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(js), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	title := func() string {
		nav, err := app.dev.plugin.Navigation(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return nav[0].Title
	}
	write("export function navigation( {")
	app.reloadDev()
	if app.dev.plugin != p || app.src != source.Source(p) || title() != "一" || !strings.Contains(app.toast, "仍在用之前的版本") {
		t.Fatalf("broken reload: plugin %p (was %p), title %q, toast %q", app.dev.plugin, p, title(), app.toast)
	}
	write(devJS("二"))
	app.reloadDev()
	if app.dev.plugin != p || title() != "二" {
		t.Fatalf("reload: title %q", title())
	}

	// Broken from the start: watched, and loaded once fixed.
	app.unloadDev()
	write("export function navigation( {")
	if err := app.loadDev(dir); err == nil {
		t.Fatal("broken folder loaded")
	}
	write(devJS("三"))
	deadline := time.After(10 * time.Second)
	for app.dev.plugin == nil {
		select {
		case fn := <-done:
			fn()
		case <-deadline:
			t.Fatal("the fixed folder never loaded")
		}
	}
	if title() != "三" || app.src != source.Source(app.dev.plugin) {
		t.Fatalf("fixed: title %q, shown %v", title(), app.src)
	}
}
