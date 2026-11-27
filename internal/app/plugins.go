package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/media"
	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/sources/hongguo"
)

// The sources the app knows: the built-in Hongguo and a plugin loaded
// from a folder in the developer mode (docs/plugin-design.md §13). The
// switcher and the management page come in a later step; for now the
// sidebar's menu switches between them.

// registry holds the loaded sources. The media manager and the posters
// read it from their goroutines.
type registry struct {
	mu   sync.Mutex
	list []source.Source
}

func (r *registry) get(id string) source.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.list {
		if s.Info().ID == id {
			return s
		}
	}
	return nil
}

func (r *registry) all() []source.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]source.Source(nil), r.list...)
}

func (r *registry) add(s source.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := s.Info().ID
	for i, old := range r.list {
		if old.Info().ID == id {
			r.list[i] = s
			return
		}
	}
	r.list = append(r.list, s)
}

func (r *registry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.list {
		if s.Info().ID == id {
			r.list = append(r.list[:i], r.list[i+1:]...)
			return
		}
	}
}

// mediaPolicy is what a source's media may reach: a plugin only its
// hosts, an unknown source nothing.
func (r *registry) mediaPolicy(id string) media.Policy {
	if s := r.get(id); s != nil {
		return media.PolicyFor(s.Info())
	}
	return media.Policy{Sandboxed: true}
}

// imageRoute picks how to download an image: through the plugin whose
// hosts it is on, with the plugin's image headers and none of its
// cookies; else, as an image of a built-in source, directly.
func (r *registry) imageRoute(raw string) (*http.Client, map[string]string) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil
	}
	for _, s := range r.all() {
		p, ok := s.(*plugin.Plugin)
		if !ok {
			continue
		}
		if netguard.Hosts(p.Info().Network).AllowsURL(u) {
			return p.ImageClient(), p.Info().ImageHeaders
		}
	}
	return nil, nil
}

// devState is the developer mode: a plugin folder, loaded and watched.
type devState struct {
	dir    string
	plugin *plugin.Plugin
	err    string
	stop   context.CancelFunc
	// logOpen shows the plugin's log page; logGen counts lines, to redraw.
	logOpen bool
	logGen  int
}

// devSettings is what the developer mode keeps between launches.
type devSettings struct {
	Dir     string `json:"dir,omitempty"`
	Current string `json:"current,omitempty"` // the source shown
}

func (app *App) devSettingsPath() string {
	return filepath.Join(app.dataDir, "plugins", "dev.json")
}

func (app *App) loadDevSettings() devSettings {
	var s devSettings
	if data, err := os.ReadFile(app.devSettingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	// For trying a plugin from the command line, as the tour does.
	if dir := os.Getenv("ICHIWA_PLUGIN_DIR"); dir != "" {
		s.Dir = dir
	}
	return s
}

func (app *App) saveDevSettings() {
	if app.dataDir == "" {
		return
	}
	s := devSettings{Dir: app.dev.dir}
	if app.src != nil && app.src.Info().ID != hongguo.SourceID {
		s.Current = app.src.Info().ID
	}
	path := app.devSettingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.Marshal(s)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("plugins: %v", err)
	}
}

func (app *App) pluginOptions() plugin.Options {
	return plugin.Options{
		AppVersion: mygo.App.Version(),
		DataDir:    filepath.Join(app.dataDir, "plugins", "data"),
	}
}

// loadDev loads the plugin folder dir in the developer mode and watches
// it, replacing a folder loaded before. It runs on the main thread.
func (app *App) loadDev(dir string) error {
	app.unloadDev()
	p, err := plugin.Open(dir, app.pluginOptions())
	app.dev.dir = dir
	if err != nil {
		app.dev.err = err.Error()
		return err
	}
	app.dev.plugin, app.dev.err = p, ""
	app.sources.add(p)
	ctx, cancel := context.WithCancel(context.Background())
	app.dev.stop = cancel
	go p.WatchDir(ctx, dir, 0, func(err error) {
		app.dispatch(func() {
			if app.dev.plugin != p {
				return
			}
			if err != nil {
				app.toast = "插件重新加载失败，详见插件日志"
				return
			}
			app.toast = "插件已重新加载"
			if app.src == source.Source(p) {
				app.refreshSource() // the navigation and pages may have changed
			}
		})
	})
	var redraw throttle
	stopLogs := p.Logs().Watch(func(plugin.LogEntry) {
		redraw.poke(logRedraw, func() { app.dispatch(func() { app.dev.logGen++ }) })
	})
	go func() { <-ctx.Done(); stopLogs() }()
	return nil
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

// unloadDev stops the developer mode's plugin; the app goes back to
// Hongguo when it showed it.
func (app *App) unloadDev() {
	p := app.dev.plugin
	if app.dev.stop != nil {
		app.dev.stop()
	}
	app.dev = devState{}
	if p == nil {
		return
	}
	app.sources.remove(p.Info().ID)
	if app.src == source.Source(p) {
		if h := app.sources.get(hongguo.SourceID); h != nil {
			app.useSource(h)
		}
	}
}

// pickDevFolder asks for a plugin folder and loads it.
func (app *App) pickDevFolder() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      app.win,
		Title:       "选择插件文件夹",
		Message:     "开发者模式：从文件夹加载插件，文件变化时自动重新加载",
		ButtonLabel: "加载",
		Directory:   true,
	})
	if err != nil || len(paths) == 0 {
		return
	}
	if err := app.loadDev(paths[0]); err != nil {
		app.toast = "插件加载失败：" + err.Error()
		app.dev.logOpen = false
		app.saveDevSettings()
		return
	}
	app.useSource(app.dev.plugin)
	app.toast = "已加载插件 " + app.dev.plugin.Info().Name
}

// useSource shows s: its navigation, its first page, and nothing of the
// source shown before.
func (app *App) useSource(s source.Source) {
	if app.watch != nil {
		app.closeWatch()
	}
	app.src = s
	app.nav, app.navErr = nil, ""
	app.feeds = map[string]*Feed{}
	app.cols = map[string]*colState{}
	app.filter = filterState{}
	app.rank = rankState{}
	app.search = searchState{}
	app.person = nil
	app.warmed = false
	app.dev.logOpen = false
	if app.builtin() {
		app.page = pageRecommend
	} else {
		app.page = ""
	}
	app.loadNav()
	if app.win != nil {
		app.setMenu()
	}
	app.saveDevSettings()
}

// refreshSource loads the pages of the source shown again, as a plugin
// reloaded, keeping the page shown when its navigation still has it and
// the player open.
func (app *App) refreshSource() {
	app.feeds = map[string]*Feed{}
	app.cols = map[string]*colState{}
	app.search = searchState{}
	app.person = nil
	app.warmed = false
	src, page := app.src, app.page
	app.navBusy = true
	async(app, func() ([]source.NavItem, error) {
		return src.Navigation(context.Background())
	}, func(nav []source.NavItem, err error) {
		if app.src != src {
			return
		}
		app.navBusy = false
		if err != nil {
			app.navErr = errText(err)
			return
		}
		app.nav, app.navErr = nav, ""
		switch page {
		case pageSearch, pageHistory, pageFollows:
			return
		}
		for _, it := range nav {
			if it.ID == page {
				return
			}
		}
		if len(nav) > 0 {
			app.page = nav[0].ID
		}
	})
}

// builtin tells whether the source shown is built into the app, as
// Hongguo is, whose pages the app draws its own way; a plugin's pages are
// drawn from its collections.
func (app *App) builtin() bool {
	_, isPlugin := app.src.(*plugin.Plugin)
	return !isPlugin
}

// loadNav loads the navigation of the source shown.
func (app *App) loadNav() {
	src := app.src
	if src == nil {
		return
	}
	app.navBusy = true
	async(app, func() ([]source.NavItem, error) {
		return src.Navigation(context.Background())
	}, func(nav []source.NavItem, err error) {
		if app.src != src {
			return
		}
		app.navBusy = false
		if err != nil {
			app.navErr = errText(err)
			return
		}
		app.nav = nav
		if app.page == "" && len(nav) > 0 {
			app.page = nav[0].ID
			if saved := app.lib.Page; saved != "" {
				for _, it := range nav {
					if it.ID == saved {
						app.page = saved
					}
				}
			}
		}
	})
}

// sourceMenu is the sidebar's switcher: the sources, and the developer
// mode.
func (app *App) sourceMenu(m *ui.Menu) {
	for _, s := range app.sources.all() {
		info := s.Info()
		label := info.Name
		if _, ok := s.(*plugin.Plugin); ok {
			label += "（开发中）"
		}
		if m.Item(label).ID("src-"+info.ID).Radio(app.src == s).Chosen() && app.src != s {
			app.useSource(s)
		}
	}
	m.Separator()
	m.Submenu("开发者模式", func(m *ui.Menu) {
		if m.Item("从文件夹加载插件…").Chosen() {
			app.pickDevFolder()
		}
		if app.dev.dir != "" {
			if m.Item("重新加载").Chosen() {
				if err := app.loadDev(app.dev.dir); err != nil {
					app.toast = "插件加载失败：" + err.Error()
				} else {
					app.useSource(app.dev.plugin)
				}
			}
			if app.dev.plugin != nil && m.Item("插件日志").Chosen() {
				app.dev.logOpen = true
				app.person = nil
			}
			if m.Item("关闭开发者模式").Chosen() {
				app.unloadDev()
				app.saveDevSettings()
			}
		}
	})
}

// sourceSwitch draws the switcher at the top of the sidebar.
func (app *App) sourceSwitch(c *ui.Context) {
	p := app.pal
	name := "片源"
	if app.src != nil {
		name = app.src.Info().Name
	}
	b := ui.ButtonBase(c.Key("source")).Height(34).PaddingX(10).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		Label("片源：" + name).Description("切换片源")
	pressable(b, ui.Transparent, p.hover, 0.98)
	b.Children(func() {
		ui.Text(c, name).FontSize(13.5).FontWeight(600).TextColor(p.text).Grow(1).SingleLine()
		ui.Icon(c, iconChevronDown).Size(12, 12).TextColor(p.muted)
	})
	b.Menu(app.sourceMenu)
	if app.dev.err != "" {
		ui.Text(c, "开发插件加载失败："+app.dev.err).FontSize(11).TextColor(p.muted).Padding(2, 10).MaxLines(3)
	}
}

// logPageLines is how much of the log the page shows: the newest lines.
const logPageLines = 500

// logPage shows the log of the developer mode's plugin as it grows.
func (app *App) logPage(c *ui.Context) {
	p := app.pal
	pl := app.dev.plugin
	if pl == nil {
		app.dev.logOpen = false
		return
	}
	_ = app.dev.logGen
	app.header(c, "插件日志", pl.Info().Name+" "+pl.Info().Version, func() {
		app.secondary(c, iconX, "关闭").OnClick(func() { app.dev.logOpen = false })
	})
	var lines []string
	for _, e := range pl.Logs().Tail(logPageLines) {
		// An entry may hold several lines; the page shows its last ones.
		lines = append(lines, strings.Split(e.String(), "\n")...)
	}
	if len(lines) > logPageLines {
		lines = lines[len(lines)-logPageLines:]
	}
	if len(lines) == 0 {
		lines = []string{"还没有日志"}
	}
	ui.Scroll(c.Key("plugin-log")).Grow(1).Padding(0, pagePad, 24).Children(func() {
		for i, l := range lines {
			ui.Text(c.Key(i), l).FontSize(12).TextColor(p.text).Selectable()
		}
	})
}
