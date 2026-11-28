package ui

import (
	"github.com/egoist/mygo/ui"

	core "github.com/giuem/ichiwa/internal/app"
	"github.com/giuem/ichiwa/internal/source"
)

// sourceIcons are the sources' icons as drawn, by ID; nil for a source
// whose icon does not decode.
var sourceIcons = map[string]*ui.Bitmap{}

// sourceIcon draws the icon of a source, or the first letter of its name
// when it has none.
func (app *App) sourceIcon(c *ui.Context, info source.Info, size float32) {
	p := app.pal
	bmp, ok := sourceIcons[info.ID]
	if !ok && len(info.Icon) > 0 {
		bmp, _ = ui.DecodeBitmap(info.Icon)
		sourceIcons[info.ID] = bmp
	}
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(size * 0.28).Clip().Center().AccessHidden(true)
	if bmp == nil {
		box.Background(p.accent)
	}
	box.Children(func() {
		if bmp != nil {
			ui.Image(c, bmp).Fill().Fit(ui.Cover)
			return
		}
		ui.Text(c, firstRune(info.Name)).FontSize(size * 0.55).Bold().TextColor(p.onAccent)
	})
}

// sourceName is the name of a source as the switcher lists it.
func (app *App) sourceName(s source.Source) string {
	name := s.Info().Name
	if app.DevSource(s) {
		name += "（开发中）"
	}
	return name
}

// sourceMenu lists the sources turned on, and the way to manage them.
func (app *App) sourceMenu(m *ui.Menu) {
	for _, s := range app.SourcesOn() {
		info := s.Info()
		if m.Item(app.sourceName(s)).ID("src-" + info.ID).Radio(app.Src == s).Chosen() {
			app.SwitchSource(s)
		}
	}
	m.Separator()
	if m.Item("管理片源…").Chosen() {
		app.Go(core.PageSources)
	}
}

// sourceSwitch draws the switcher at the top of the sidebar: the source
// shown, which opens the list of the others; with none, the way to add one.
func (app *App) sourceSwitch(c *ui.Context) {
	p := app.pal
	if app.Src == nil {
		b := ui.ButtonBase(c.Key("source")).Height(38).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
			Label("添加片源")
		if app.Page == core.PageSources {
			b.Current(ui.CurrentPage)
		}
		pressable(b, ui.Transparent, p.hover, 0.98)
		b.Children(func() {
			ui.Box(c).Size(24, 24).Radius(7).Center().Background(p.accent.Alpha(0.12)).Children(func() {
				ui.Text(c, "+").FontSize(16).Bold().TextColor(p.accent)
			})
			ui.Text(c, "添加片源").FontSize(13.5).FontWeight(600).TextColor(p.text).Grow(1).SingleLine()
		})
		if b.Clicked() {
			app.Go(core.PageSources)
		}
		return
	}
	info := app.Src.Info()
	name := app.sourceName(app.Src)
	b := ui.ButtonBase(c.Key("source")).Height(38).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		Label("片源：" + name).Description("切换片源")
	pressable(b, ui.Transparent, p.hover, 0.98)
	b.Children(func() {
		app.sourceIcon(c, info, 24)
		ui.Text(c, name).FontSize(13.5).FontWeight(600).TextColor(p.text).Grow(1).SingleLine()
		ui.Icon(c, iconChevronDown).Size(12, 12).TextColor(p.muted)
	})
	b.Menu(app.sourceMenu)
	if app.Dev.Err != "" {
		ui.Text(c, "开发插件加载失败："+app.Dev.Err).FontSize(11).TextColor(p.muted).Padding(2, 10).MaxLines(3)
	}
}

// guidePage shows, while no source is on, what sources are and where to
// add one.
func (app *App) guidePage(c *ui.Context) {
	p := app.pal
	app.tell(c, "还没有片源")
	ui.Box(c).Height(max(c.TitleBar().Height, 28) + 10).DragWindow()
	ui.Column(c).Grow(1).Center().Gap(10).Padding(40).Children(func() {
		ui.Box(c).Size(64, 64).Radius(999).Background(p.surface).Center().Margin(0, 0, 6).Children(func() {
			ui.Icon(c, iconFilm).Size(26, 26).TextColor(p.muted)
		})
		ui.Text(c, "还没有片源").FontSize(16).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(1)
		ui.Text(c, "片源提供可以看的剧。添加一个片源，就能在这里浏览、搜索和播放。").FontSize(13).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
		app.primary(c, nil, "管理片源").Margin(10, 0, 0).OnClick(func() { app.Go(core.PageSources) })
	})
}

// sourcesPage manages the sources: which are on, and the developer mode.
func (app *App) sourcesPage(c *ui.Context) {
	p := app.pal
	app.header(c, "片源", "打开的片源会出现在侧栏顶部，可以随时切换", nil)
	ui.Scroll(c.Key("sources")).Grow(1).Padding(0, pagePad, 28).Gap(28).Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			app.sectionTitle(c, "已安装")
			all := app.Sources.All()
			if len(all) == 0 {
				ui.Text(c, "还没有安装片源").FontSize(13).TextColor(p.muted)
			}
			for _, s := range all {
				app.sourceRow(c, s)
			}
		})
	})
}

// sectionTitle is the title of a part of a page.
func (app *App) sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).Bold().TextColor(app.pal.text).Role(ui.RoleHeading).Level(2)
}

// sourceRow is a source on the sources page: its icon, name and what it
// is, and whether it is on.
func (app *App) sourceRow(c *ui.Context, s source.Source) {
	p := app.pal
	info := s.Info()
	id := info.ID
	kind := "内置"
	if app.DevSource(s) {
		kind = "开发者模式"
	}
	ui.Row(c.Key("src-"+id)).Gap(14).Padding(14, 16).Radius(14).Background(p.surface).AlignItems(ui.Center).MaxWidth(720).Children(func() {
		app.sourceIcon(c, info, 40)
		ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, info.Name).FontSize(14.5).FontWeight(600).TextColor(p.text).SingleLine()
				if app.Src == s {
					ui.Text(c, "正在浏览").FontSize(11).FontWeight(600).TextColor(p.accent).Padding(1, 6).Radius(5).Background(p.accent.Alpha(0.12))
				}
			})
			ui.Text(c, joinNonEmpty(kind, ternary(info.Version != "", "版本 "+info.Version, ""))).FontSize(12).TextColor(p.muted).SingleLine()
			if info.Description != "" {
				ui.Text(c, info.Description).FontSize(12).TextColor(p.muted).MaxLines(2)
			}
		})
		on := app.SourceOn(id)
		ui.Switch(c.Key("on"), &on).Label("打开" + info.Name).OnClick(func() {
			app.SetSourceOn(id, on)
			app.Toast = ternary(on, "已打开"+info.Name, "已关闭"+info.Name)
		})
	})
}
