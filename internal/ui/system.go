package ui

import (
	"bytes"
	"image/jpeg"
	"time"

	core "github.com/giuem/ichiwa/internal/app"

	"github.com/egoist/mygo"

	"github.com/giuem/ichiwa/internal/source"
)

// setMenu gives the app its menu bar: the app menu with updates, the
// editing commands text fields need, the player's controls and the window.
func (app *App) setMenu() {
	run := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			fn()
			app.win.Invalidate()
		}
	}
	onWatch := func(fn func(w *Watching)) func() {
		return func() {
			if app.Watch != nil {
				fn(app.Watch)
			}
		}
	}
	editSubmenu := []*mygo.MenuItem{
		{Role: mygo.RoleUndo, Label: "撤销"},
		{Role: mygo.RoleRedo, Label: "重做"},
		mygo.Separator(),
		{Role: mygo.RoleCut, Label: "剪切"},
		{Role: mygo.RoleCopy, Label: "拷贝"},
		{Role: mygo.RolePaste, Label: "粘贴"},
		{Role: mygo.RoleSelectAll, Label: "全选"},
	}
	if app.HasCap(source.CapSearch) {
		editSubmenu = append(editSubmenu,
			mygo.Separator(),
			&mygo.MenuItem{Label: "搜索", Accelerator: "CmdOrCtrl+F", Click: run(func() { app.Go(core.PageSearch); app.Search.Focus = true })},
		)
	}
	name := "一集"
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: name, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: "关于" + name},
			{Label: "检查更新…", Click: run(func() { go app.checkForUpdates(true) })},
			mygo.Separator(),
			{Role: mygo.RoleServices, Label: "服务"},
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: "隐藏" + name},
			{Role: mygo.RoleHideOthers, Label: "隐藏其他"},
			{Role: mygo.RoleUnhide, Label: "全部显示"},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: "退出" + name},
		}},
		{Label: "编辑", Submenu: editSubmenu},
		{Label: "播放", Submenu: []*mygo.MenuItem{
			{Label: "播放/暂停", Click: run(onWatch(func(w *Watching) { w.TogglePlay() }))},
			{Label: "上一集", Accelerator: "CmdOrCtrl+Up", Click: run(onWatch(func(w *Watching) { w.Step(-1) }))},
			{Label: "下一集", Accelerator: "CmdOrCtrl+Down", Click: run(onWatch(func(w *Watching) { w.Step(1) }))},
			mygo.Separator(),
			{Label: "快进 10 秒", Accelerator: "CmdOrCtrl+Right", Click: run(onWatch(func(w *Watching) { w.SeekBy(10) }))},
			{Label: "快退 10 秒", Accelerator: "CmdOrCtrl+Left", Click: run(onWatch(func(w *Watching) { w.SeekBy(-10) }))},
			mygo.Separator(),
			{Label: "追剧/取消追剧", Accelerator: "CmdOrCtrl+D", Click: run(onWatch(func(w *Watching) { app.ToggleFollow(w.SourceID, w.Show) }))},
			{Label: "返回", Accelerator: "CmdOrCtrl+[", Click: run(app.CloseWatch)},
		}},
		{Label: "窗口", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: "最小化"},
			{Role: mygo.RoleZoom, Label: "缩放"},
			{Role: mygo.RoleToggleFullScreen, Label: "进入全屏"},
			{Role: mygo.RoleClose, Label: "关闭"},
			mygo.Separator(),
			{Role: mygo.RoleFront, Label: "前置全部窗口"},
		}},
	}))
}

// mediaKeys lets the media keys and Control Center drive the player.
func (app *App) mediaKeys() {
	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		mygo.RunOnMain(func() {
			w := app.Watch
			if w == nil || w.Player == nil {
				return
			}
			switch cmd {
			case mygo.MediaPlay:
				if !w.Player.Playing() {
					w.TogglePlay()
				}
			case mygo.MediaPause, mygo.MediaStop:
				if w.Player.Playing() {
					w.TogglePlay()
				}
			case mygo.MediaNext:
				w.Step(1)
			case mygo.MediaPrevious:
				w.Step(-1)
			case mygo.MediaSeek:
				w.Player.Seek(pos)
				w.SyncNowPlaying()
			}
			app.win.Invalidate()
		})
	})
}

// nowPlaying shows the episode in Control Center.

// NowPlaying shows the episode in Control Center, nil for nothing.
func (app *App) NowPlaying(n *core.NowPlaying) {
	if n == nil {
		mygo.NowPlaying.Clear()
		return
	}
	info := mygo.NowPlayingInfo{
		Title:    n.Title,
		Artist:   n.Artist,
		Album:    "一集",
		Duration: n.Duration,
		Position: n.Position,
		Rate:     n.Rate,
		State:    mygo.PlaybackPlaying,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaSeek | mygo.MediaNext | mygo.MediaPrevious,
	}
	if img := app.posters.Image(n.Image); img != nil {
		var buf bytes.Buffer
		if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}) == nil {
			info.Artwork = buf.Bytes()
		}
	}
	mygo.NowPlaying.Set(info)
}

// Playback tells Control Center where playback is.
func (app *App) Playback(playing bool, at time.Duration) {
	state := mygo.PlaybackPaused
	if playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, at)
}
