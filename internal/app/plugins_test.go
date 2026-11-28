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

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// Images and media of a plugin go through its own policy; a built-in
// source's take the plain client; an unknown source reaches nothing.
func TestSourcePolicies(t *testing.T) {
	p, err := plugin.Open(filepath.Join("..", "plugin", "testdata", "example"), plugin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var r registry
	r.Add(&sourcetest.Source{InfoData: source.Info{ID: "hongguo"}})
	r.Add(p)
	if c, h := r.ImageRoute("https://picsum.photos/seed/x/300/400"); c == nil || h["Referer"] == "" {
		t.Fatal("a plugin's image does not go through the plugin")
	}
	if c, h := r.ImageRoute("https://p3-hongguo.example/cover.jpg"); c == nil || c.Transport == p.ImageClient().Transport || h != nil {
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
		c, _ := r.ImageRoute(u)
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
		Src:     builtin,
		Lib:     testLibrary(t),
		DataDir: t.TempDir(),
		post:    func(fn func()) { done <- fn },
		Search:  &searchState{},
	}
	app.Sources.Add(builtin)
	t.Cleanup(app.UnloadDev)
	return app, builtin, done
}

// A plugin in the developer mode with the id of a built-in source stands
// in for it while loaded, and the built-in one comes back after.
func TestDevPluginStandsInForBuiltin(t *testing.T) {
	app, builtin, _ := devApp(t)
	dir := writeDevDir(t, devJS("一"))
	if err := app.LoadDev(dir); err != nil {
		t.Fatal(err)
	}
	p := app.Dev.Plugin
	if app.Src != source.Source(p) || app.Sources.Get("hongguo") != source.Source(p) {
		t.Fatalf("shown %v, registered %v", app.Src, app.Sources.Get("hongguo"))
	}
	if all := app.Sources.All(); len(all) != 1 || all[0] != source.Source(p) {
		t.Fatalf("sources = %v", all)
	}
	// The folder is saved, and the source shown.
	if s := app.loadDevSettings(); s.Dir != dir || app.Lib.Source != "hongguo" {
		t.Fatalf("saved %+v, shown %q", s, app.Lib.Source)
	}
	if pol := app.Sources.mediaPolicy("hongguo"); !pol.Sandboxed {
		t.Fatal("the plugin's media are not sandboxed")
	}

	app.UnloadDev()
	app.SaveDevSettings()
	if app.Src != builtin || app.Sources.Get("hongguo") != builtin {
		t.Fatalf("after unloading: shown %v, registered %v", app.Src, app.Sources.Get("hongguo"))
	}
	if all := app.Sources.All(); len(all) != 1 || all[0] != builtin {
		t.Fatalf("sources = %v", all)
	}
	if s := app.loadDevSettings(); s.Dir != "" {
		t.Fatalf("saved %+v", s)
	}
	if pol := app.Sources.mediaPolicy("hongguo"); pol.Sandboxed {
		t.Fatal("the built-in source's media are sandboxed")
	}

	// Loaded while another source is shown, it does not take over.
	other := &sourcetest.Source{InfoData: source.Info{ID: "other", Name: "别的"}}
	app.Sources.Add(other)
	app.UseSource(other)
	if err := app.LoadDev(dir); err != nil {
		t.Fatal(err)
	}
	if app.Src != source.Source(other) || len(app.Sources.All()) != 2 {
		t.Fatalf("shown %v, sources %v", app.Src, app.Sources.All())
	}
	if app.Lib.Source != "other" {
		t.Fatalf("saved %q as shown", app.Lib.Source)
	}
}

// The menu's reload keeps the plugin and its code when the folder is
// broken, and a folder that did not load loads once it is fixed.
func TestDevReloadKeepsLastGoodCode(t *testing.T) {
	app, _, done := devApp(t)
	dir := writeDevDir(t, devJS("一"))
	if err := app.LoadDev(dir); err != nil {
		t.Fatal(err)
	}
	p := app.Dev.Plugin
	write := func(js string) {
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(js), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	title := func() string {
		nav, err := app.Dev.Plugin.Navigation(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return nav[0].Title
	}
	write("export function navigation( {")
	app.ReloadDev()
	if app.Dev.Plugin != p || app.Src != source.Source(p) || title() != "一" || !strings.Contains(app.Toast, "仍在用之前的版本") {
		t.Fatalf("broken reload: plugin %p (was %p), title %q, toast %q", app.Dev.Plugin, p, title(), app.Toast)
	}
	write(devJS("二"))
	app.ReloadDev()
	if app.Dev.Plugin != p || title() != "二" {
		t.Fatalf("reload: title %q", title())
	}

	// Broken from the start: watched, and loaded once fixed.
	app.UnloadDev()
	write("export function navigation( {")
	if err := app.LoadDev(dir); err == nil {
		t.Fatal("broken folder loaded")
	}
	write(devJS("三"))
	deadline := time.After(10 * time.Second)
	for app.Dev.Plugin == nil {
		select {
		case fn := <-done:
			fn()
		case <-deadline:
			t.Fatal("the fixed folder never loaded")
		}
	}
	if title() != "三" || app.Src != source.Source(app.Dev.Plugin) {
		t.Fatalf("fixed: title %q, shown %v", title(), app.Src)
	}
}

// A first page whose list failed shows what it has, and the retry's
// first page replaces it whole: header, sections and filters.
func TestRetryReplacesFailedFirstPage(t *testing.T) {
	cs := &Collection{ID: "x"}
	failed := &source.CollectionPage{Filters: []FilterRow{{Key: "old", Title: "旧"}}}
	cs.applyPage(feedPage{Page: failed, failed: true})
	if cs.First != failed || len(cs.Rows) != 1 {
		t.Fatalf("failed page not shown: %+v", cs)
	}
	good := &source.CollectionPage{
		Header:   &source.Header{Title: "新的"},
		Sections: []Section{{Title: "分区"}},
		Filters:  []FilterRow{{Key: "new", Title: "新"}, {Key: "more", Title: "多"}},
	}
	cs.applyPage(feedPage{Page: good})
	if cs.First != good || len(cs.Rows) != 2 || cs.Rows[0].Key != "new" {
		t.Fatalf("retry's page dropped: %+v", cs)
	}
	// Later pages do not replace the first.
	cs.applyPage(feedPage{Page: &source.CollectionPage{}})
	if cs.First != good {
		t.Fatal("a later page replaced the first")
	}
}

// A plugin reloaded while another source shows comes back with its new
// navigation and pages, not those it showed before.
func TestDevReloadedAwayShowsAnew(t *testing.T) {
	app, _, done := devApp(t)
	other := navSource("other", "o1")
	app.Sources.Add(other)
	app.UseSource(other)
	dir := writeDevDir(t, devJS("一"))
	if err := app.LoadDev(dir); err != nil {
		t.Fatal(err)
	}
	p := app.Dev.Plugin
	run := func() {
		for app.NavBusy {
			select {
			case fn := <-done:
				fn()
			case <-time.After(10 * time.Second):
				t.Fatal("the navigation never came")
			}
		}
	}
	app.SwitchSource(p)
	run()
	if len(app.Nav) != 1 || app.Nav[0].Title != "一" {
		t.Fatalf("nav = %+v", app.Nav)
	}
	app.SwitchSource(other)
	run()
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(devJS("二")), 0o644); err != nil {
		t.Fatal(err)
	}
	app.ReloadDev()
	app.SwitchSource(p)
	run()
	if len(app.Nav) != 1 || app.Nav[0].Title != "二" {
		t.Fatalf("after the reload, nav = %+v", app.Nav)
	}
}
