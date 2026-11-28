package ui

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	core "github.com/giuem/ichiwa/internal/app"
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
