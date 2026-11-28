package ui

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
	app.SetSourceOn(app.Dev.Plugin.Info().ID, true)
	app.UseSource(app.Dev.Plugin)
	app.Toast = "已加载插件 " + app.Dev.Plugin.Info().Name
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
