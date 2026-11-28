package ui

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/plugin"
)

// userLocale is the language the user reads first, for plugins, which
// may serve content in it; zh-CN when the system does not say.
func userLocale() string {
	if langs := mygo.App.PreferredLanguages(); len(langs) > 0 && langs[0] != "" {
		return langs[0]
	}
	return "zh-CN"
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
	if err := app.LoadDev(paths[0]); err != nil {
		app.Toast = "插件加载失败：" + err.Error()
		app.Dev.LogOpen = false
		app.SaveDevSettings()
		return
	}
	app.UseSource(app.Dev.Plugin)
	app.Toast = "已加载插件 " + app.Dev.Plugin.Info().Name
}

// sourceMenu is the sidebar's switcher: the sources, and the developer
// mode.
func (app *App) sourceMenu(m *ui.Menu) {
	for _, s := range app.Sources.All() {
		info := s.Info()
		label := info.Name
		if _, ok := s.(*plugin.Plugin); ok {
			label += "（开发中）"
		}
		if m.Item(label).ID("src-"+info.ID).Radio(app.Src == s).Chosen() && app.Src != s {
			app.UseSource(s)
		}
	}
	m.Separator()
	m.Submenu("开发者模式", func(m *ui.Menu) {
		if m.Item("从文件夹加载插件…").Chosen() {
			app.pickDevFolder()
		}
		if app.Dev.Dir != "" {
			if m.Item("重新加载").Chosen() {
				app.ReloadDev()
			}
			if app.Dev.Plugin != nil && m.Item("插件日志").Chosen() {
				app.Dev.LogOpen = true
			}
			if m.Item("关闭开发者模式").Chosen() {
				app.UnloadDev()
				app.SaveDevSettings()
			}
		}
	})
}

// sourceSwitch draws the switcher at the top of the sidebar.
func (app *App) sourceSwitch(c *ui.Context) {
	p := app.pal
	name := "片源"
	if app.Src != nil {
		name = app.Src.Info().Name
	}
	b := ui.ButtonBase(c.Key("source")).Height(34).PaddingX(10).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		Label("片源：" + name).Description("切换片源")
	pressable(b, ui.Transparent, p.hover, 0.98)
	b.Children(func() {
		ui.Text(c, name).FontSize(13.5).FontWeight(600).TextColor(p.text).Grow(1).SingleLine()
		ui.Icon(c, iconChevronDown).Size(12, 12).TextColor(p.muted)
	})
	b.Menu(app.sourceMenu)
	if app.Dev.Err != "" {
		ui.Text(c, "开发插件加载失败："+app.Dev.Err).FontSize(11).TextColor(p.muted).Padding(2, 10).MaxLines(3)
	}
}

// logPageLines is how much of the log the page shows: the newest lines.
const logPageLines = 500

// logPage shows the log of the developer mode's plugin as it grows.
func (app *App) logPage(c *ui.Context) {
	p := app.pal
	pl := app.Dev.Plugin
	if pl == nil {
		app.Dev.LogOpen = false
		return
	}
	_ = app.Dev.LogGen
	app.header(c, "插件日志", pl.Info().Name+" "+pl.Info().Version, func() {
		app.secondary(c, iconX, "关闭").OnClick(func() { app.Dev.LogOpen = false })
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
