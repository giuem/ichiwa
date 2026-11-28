package ui

import (
	"github.com/egoist/mygo"
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
		app.SaveDevSettings()
		return
	}
	app.SetSourceOn(app.Dev.Plugin.Info().ID, true)
	app.UseSource(app.Dev.Plugin)
	app.Toast = "已加载插件 " + app.Dev.Plugin.Info().Name
}

// logPageLines is how much of the log the page shows: the newest lines.
