package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	core "github.com/giuem/ichiwa/internal/app"
	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

func namedSource(id, name string) *sourcetest.Source {
	return &sourcetest.Source{
		InfoData: source.Info{ID: id, Name: name},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			return []source.NavItem{{ID: id + "-home", Title: "首页"}}, nil
		},
	}
}

// The switcher says which source shows and lists those on, with the way
// to manage them; choosing one switches to it.
func TestSourceSwitcher(t *testing.T) {
	a, b, off := namedSource("a", "甲片源"), namedSource("b", "乙片源"), namedSource("c", "丙片源")
	app := viewApp(t, lightPalette(), a, b, off)
	app.Lib.Off["c"] = true
	tt := ui.NewTester(app.sidebar, 220, 600)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleMenuButton, "片源：甲片源"); a.Description != "切换片源" {
		t.Errorf("the switcher: %+v", a)
	}
	if err := tt.Click("片源：甲片源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Menu(); len(got) != 4 || got[0] != "甲片源" || got[1] != "乙片源" || got[3] != "管理片源…" {
		t.Fatalf("menu = %q", got)
	}
	if err := tt.ChooseMenuItem("乙片源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Src != source.Source(b) {
		t.Fatalf("shown %v", app.Src)
	}
	accessOf(t, tt, ui.RoleMenuButton, "片源：乙片源")
}

// With no source on, the sidebar and the page say how to add one.
func TestNoSourceGuides(t *testing.T) {
	app := viewApp(t, lightPalette())
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(sidebarWidth).Children(func() { app.sidebar(c) })
			ui.Column(c).Grow(1).Children(func() { app.content(c) })
		})
	}, 1100, 700)
	tt.Frame()
	accessOf(t, tt, ui.RoleButton, "添加片源")
	accessOf(t, tt, ui.RoleHeading, "还没有片源")
	if _, ok := tt.AccessByRole(ui.RoleButton, "搜索"); ok {
		t.Error("search shows without a source")
	}
	if err := tt.Click("管理片源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Page != core.PageSources {
		t.Fatalf("page = %q", app.Page)
	}
	accessOf(t, tt, ui.RoleHeading, "片源")
}

// The sources page turns a source off and on with a switch the keyboard
// and screen readers reach.
func TestSourcesPageSwitches(t *testing.T) {
	a, b := namedSource("a", "甲片源"), namedSource("b", "乙片源")
	app := viewApp(t, lightPalette(), a, b)
	app.Go(core.PageSources)
	tt := ui.NewTester(app.content, 900, 600)
	tt.Frame()
	sw := accessOf(t, tt, ui.RoleSwitch, "打开甲片源")
	if !sw.Checked {
		t.Fatal("a source on says it is off")
	}
	if err := tt.Click("打开甲片源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.SourceOn("a") || app.Src != source.Source(b) {
		t.Fatalf("turned off: on %v, shown %v", app.SourceOn("a"), app.Src)
	}
	if sw := accessOf(t, tt, ui.RoleSwitch, "打开甲片源"); sw.Checked {
		t.Error("a source off says it is on")
	}
}

// Records of every source show together, each saying its source; one of
// a source turned off or not installed says it can't open, and opening it
// tells why and leads to the sources page.
func TestRecordsAcrossSources(t *testing.T) {
	a, b := namedSource("a", "甲片源"), namedSource("b", "乙片源")
	app := viewApp(t, lightPalette(), a, b)
	app.Lib.Off["b"] = true
	app.Lib.Progress("a", Card{Kind: source.CardMedia, ID: "1", Title: "甲剧"}, "e1", 0, 10, 30, 100)
	app.Lib.Progress("b", Card{Kind: source.CardMedia, ID: "2", Title: "乙剧"}, "e1", 0, 10, 30, 100)
	app.Lib.Progress("gone", Card{Kind: source.CardMedia, ID: "3", Title: "丙剧"}, "e1", 0, 10, 30, 100)
	app.Go(core.PageHistory)
	tt := ui.NewTester(app.view, 1100, 700)
	tt.Frame()
	if d := accessOf(t, tt, ui.RoleButton, "甲剧").Description; !strings.Contains(d, "甲片源") || strings.Contains(d, "不可用") {
		t.Errorf("甲剧 says %q", d)
	}
	if d := accessOf(t, tt, ui.RoleButton, "乙剧").Description; !strings.Contains(d, "乙片源，片源不可用") {
		t.Errorf("乙剧 says %q", d)
	}
	if d := accessOf(t, tt, ui.RoleButton, "丙剧").Description; !strings.Contains(d, "gone，片源不可用") {
		t.Errorf("丙剧 says %q", d)
	}
	if err := tt.Click("乙剧"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if app.Watch != nil || app.Unavailable == nil || !app.Unavailable.Installed {
		t.Fatalf("watch %v, unavailable %+v", app.Watch, app.Unavailable)
	}
	accessOf(t, tt, ui.RoleHeading, "片源不可用")
	if err := tt.Click("管理片源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if app.Page != core.PageSources || app.Unavailable != nil {
		t.Fatalf("page %q, unavailable %+v", app.Page, app.Unavailable)
	}

	// A source not installed says so, and promises nothing it can't do.
	app.Go(core.PageHistory)
	tt.Frame()
	if err := tt.Click("丙剧"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	said := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(said, "没有安装") || strings.Contains(said, "装上以后") {
		t.Errorf("the dialog says %q", said)
	}
}

// A plugin's log opens from its row on the sources page and goes back;
// a built-in source has none. The developer mode offers a folder.
func TestSourcesPageLogs(t *testing.T) {
	p, err := plugin.Open(filepath.Join("..", "plugin", "testdata", "example"), plugin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Navigation(context.Background()); err != nil { // something to log
		t.Fatal(err)
	}
	builtin := namedSource("hongguo", "红果短剧")
	app := viewApp(t, lightPalette(), builtin, p)
	app.Go(core.PageSources)
	tt := ui.NewTester(app.content, 1000, 700)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "红果短剧的日志"); ok {
		t.Error("a built-in source offers a log")
	}
	accessOf(t, tt, ui.RoleButton, "选择文件夹…")
	if err := tt.Click("示例片源的日志"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	accessOf(t, tt, ui.RoleHeading, "示例片源的日志")
	if lines, _ := app.LogLines("example", 10); len(lines) == 0 {
		t.Fatal("no lines")
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.LogOf != "" {
		t.Fatal("back left the log open")
	}
	accessOf(t, tt, ui.RoleHeading, "片源")
}
