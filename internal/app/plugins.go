package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giuem/ichiwa/internal/media"
	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
)

// The sources the app knows: the built-in Hongguo and a plugin loaded
// from a folder in the developer mode (docs/plugin-design.md §13). The
// switcher and the management page come in a later step; for now the
// sidebar's menu switches between them.

// registry holds the loaded sources: the built-in ones, and the
// developer mode's plugin, which stands in for a built-in source of its
// id while it is loaded. The media manager and the posters read it from
// their goroutines.
type registry struct {
	mu      sync.Mutex
	builtin []source.Source
	Dev     source.Source
	// guard checks where images not of a loaded plugin go; nil uses the
	// system resolver.
	guard   *netguard.Guard
	guarded *http.Client
}

func (r *registry) Get(id string) source.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Dev != nil && r.Dev.Info().ID == id {
		return r.Dev
	}
	for _, s := range r.builtin {
		if s.Info().ID == id {
			return s
		}
	}
	return nil
}

// all returns the sources to choose from: the built-in ones, with the
// developer mode's plugin in place of the one of its id, or after them.
func (r *registry) All() []source.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]source.Source, 0, len(r.builtin)+1)
	placed := r.Dev == nil
	for _, s := range r.builtin {
		if r.Dev != nil && s.Info().ID == r.Dev.Info().ID {
			s, placed = r.Dev, true
		}
		out = append(out, s)
	}
	if !placed {
		out = append(out, r.Dev)
	}
	return out
}

// add adds a source, replacing one of the same id.
func (r *registry) Add(s source.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := s.Info().ID
	for i, old := range r.builtin {
		if old.Info().ID == id {
			r.builtin[i] = s
			return
		}
	}
	r.builtin = append(r.builtin, s)
}

// setDev sets the developer mode's plugin; nil unloads it, and a built-in
// source of its id comes back.
func (r *registry) setDev(s source.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Dev = s
}

// mediaPolicy is what a source's media may reach: a plugin only its
// hosts, an unknown source nothing.
func (r *registry) mediaPolicy(id string) media.Policy {
	if s := r.Get(id); s != nil {
		return media.PolicyFor(s.Info())
	}
	return media.Policy{Sandboxed: true}
}

// imageRoute picks how to download an image: through the plugin whose
// hosts it is on, with the plugin's image headers and none of its
// cookies. Any other image, of a built-in source or of a plugin not
// loaded now (a record, a follow), goes through the guard: to any host,
// but never to this machine or its network, every redirect included.
func (r *registry) ImageRoute(raw string) (*http.Client, map[string]string) {
	if u, err := url.Parse(raw); err == nil {
		for _, s := range r.All() {
			if p, ok := s.(*plugin.Plugin); ok && netguard.Hosts(p.Info().Network).AllowsURL(u) {
				return p.ImageClient(), p.Info().ImageHeaders
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.guarded == nil {
		r.guarded = &http.Client{
			Transport: r.guard.Transport(),
			CheckRedirect: netguard.CheckRedirect(func(u *url.URL) bool {
				return (u.Scheme == "http" || u.Scheme == "https") && u.User == nil
			}),
		}
	}
	return r.guarded, nil
}

// devState is the developer mode: a plugin folder, loaded and watched.
type devState struct {
	Dir    string
	Plugin *plugin.Plugin
	Err    string
	stop   context.CancelFunc
	// logOpen shows the plugin's log page; logGen counts lines, to redraw.
	LogOpen bool
	LogGen  int
}

// devSettings is what the developer mode keeps between launches.
type devSettings struct {
	Dir string `json:"dir,omitempty"`
	// Current is the source an earlier version showed, which the
	// library keeps now.
	Current string `json:"current,omitempty"`
	Env     bool   `json:"-"` // Dir comes from ICHIWA_PLUGIN_DIR
}

func (app *App) devSettingsPath() string {
	return filepath.Join(app.DataDir, "plugins", "dev.json")
}

func (app *App) loadDevSettings() devSettings {
	var s devSettings
	if data, err := os.ReadFile(app.devSettingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	// For trying a plugin from the command line, as the tour does.
	if dir := os.Getenv("ICHIWA_PLUGIN_DIR"); dir != "" {
		s.Dir, s.Env = dir, true
	}
	return s
}

func (app *App) SaveDevSettings() {
	if app.DataDir == "" {
		return
	}
	s := devSettings{Dir: app.Dev.Dir}
	path := app.devSettingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.Marshal(s)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("plugins: %v", err)
	}
}

func (app *App) pluginOptions() plugin.Options {
	return plugin.Options{
		AppVersion: app.version,
		Locale:     app.locale,
		DataDir:    filepath.Join(app.DataDir, "plugins", "data"),
	}
}

// loadDev loads the plugin folder dir in the developer mode and watches
// it, replacing a folder loaded before. A folder that does not load is
// watched still, and loads once it is fixed. It runs on the main thread.
func (app *App) LoadDev(dir string) error {
	app.UnloadDev()
	ctx, cancel := context.WithCancel(context.Background())
	app.Dev.Dir, app.Dev.stop = dir, cancel
	p, err := plugin.Open(dir, app.pluginOptions())
	if err != nil {
		app.Dev.Err = err.Error()
		plugin.WatchFolder(ctx, dir, 0, func() {
			app.dispatch(func() {
				if ctx.Err() != nil || app.Dev.Plugin != nil {
					return
				}
				app.RetryDev()
			})
		})
		app.SaveDevSettings()
		return err
	}
	app.Dev.Plugin, app.Dev.Err = p, ""
	app.Sources.setDev(p)
	go p.WatchDir(ctx, dir, 0, func(err error) {
		app.dispatch(func() {
			if app.Dev.Plugin != p {
				return
			}
			app.reloaded(p, err)
		})
	})
	var redraw throttle
	stopLogs := p.Logs().Watch(func(plugin.LogEntry) {
		redraw.poke(logRedraw, func() { app.dispatch(func() { app.Dev.LogGen++ }) })
	})
	go func() { <-ctx.Done(); stopLogs() }()
	// It stands in for the built-in source of its id, if that is shown.
	if app.Src != nil && app.Src != source.Source(p) && app.Src.Info().ID == p.Info().ID {
		app.UseSource(p)
	}
	app.SaveDevSettings()
	return nil
}

// retryDev loads the developer mode's folder again after it failed to
// load, and shows the plugin once it does.
func (app *App) RetryDev() {
	if err := app.LoadDev(app.Dev.Dir); err != nil {
		app.Toast = "插件加载失败：" + err.Error()
		return
	}
	app.UseSource(app.Dev.Plugin)
	app.Toast = "已加载插件 " + app.Dev.Plugin.Info().Name
}

// reloadDev is the menu's reload: the plugin loaded reloads in place, and
// keeps the code it ran when the folder does not load; a folder that did
// not load, or whose plugin changed its id, loads anew.
func (app *App) ReloadDev() {
	p := app.Dev.Plugin
	if p == nil {
		app.RetryDev()
		return
	}
	if pkg, err := plugin.ReadDir(app.Dev.Dir, app.version); err == nil && pkg.Manifest.ID != p.Info().ID {
		app.RetryDev()
		return
	}
	app.reloaded(p, p.ReloadDir(app.Dev.Dir))
}

// reloaded tells how a reload of the developer mode's plugin went, and
// loads the pages of the plugin again when it is shown.
func (app *App) reloaded(p *plugin.Plugin, err error) {
	if err != nil {
		app.Toast = "插件重新加载失败，仍在用之前的版本，详见插件日志"
		return
	}
	app.Toast = "插件已重新加载"
	if app.Src == source.Source(p) {
		app.refreshSource() // the navigation and pages may have changed
	}
}

// logRedraw is how often at most the log page redraws as lines come.
const logRedraw = 200 * time.Millisecond

// throttle runs a function once a while after it is poked, however
// often it is poked meanwhile.
type throttle struct{ pending atomic.Bool }

func (t *throttle) poke(after time.Duration, fn func()) {
	if t.pending.Swap(true) {
		return
	}
	time.AfterFunc(after, func() {
		t.pending.Store(false)
		fn()
	})
}

// unloadDev stops the developer mode's plugin. When the app showed it,
// it goes back to the built-in source the plugin stood in for, or to
// Hongguo.
func (app *App) UnloadDev() {
	p := app.Dev.Plugin
	if app.Dev.stop != nil {
		app.Dev.stop()
	}
	app.Dev = devState{}
	if p == nil {
		return
	}
	app.Sources.setDev(nil)
	if app.Src == source.Source(p) {
		// The built-in source it stood in for, or another.
		back := app.Sources.Get(p.Info().ID)
		if back == nil || !app.SourceOn(back.Info().ID) {
			back = app.firstOn()
		}
		app.UseSource(back)
	}
	delete(app.kept, p.Info().ID)
}

// refreshSource loads the pages of the source shown again, as a plugin
// reloaded, keeping the page shown when its navigation still has it and
// the player open.
func (app *App) refreshSource() {
	app.leaveSource()
	app.cols = map[string]*Collection{}
	app.Search = searchState{}
	app.Stack = nil
	app.warmed = false
	src, page := app.Src, app.Page
	ctx := app.srcContext()
	app.NavBusy = true
	async(app, func() ([]source.NavItem, error) {
		return src.Navigation(ctx)
	}, func(nav []source.NavItem, err error) {
		if app.Src != src || ctx.Err() != nil {
			return
		}
		app.NavBusy = false
		if err != nil {
			app.NavErr = errText(err)
			return
		}
		app.Nav, app.NavErr = nav, ""
		if appPage(page) || page == PageSearch {
			return
		}
		for _, it := range nav {
			if it.ID == page {
				return
			}
		}
		app.Page = startPage(nav, "", false)
		app.srcPage = app.Page
	})
}

// loadNav loads the navigation of the source shown.
func (app *App) LoadNav() {
	src := app.Src
	if src == nil {
		return
	}
	ctx := app.srcContext()
	app.NavBusy = true
	async(app, func() ([]source.NavItem, error) {
		return src.Navigation(ctx)
	}, func(nav []source.NavItem, err error) {
		if app.Src != src || ctx.Err() != nil {
			return
		}
		app.NavBusy = false
		if err != nil {
			app.NavErr = errText(err)
			return
		}
		app.Nav = nav
		if app.Page == "" {
			app.Page = startPage(nav, app.srcPage, app.HasCap(source.CapSearch))
			app.srcPage, app.Lib.Page = app.Page, app.Page
		}
	})
}
