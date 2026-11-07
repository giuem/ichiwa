package main

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"time"

	"github.com/egoist/mygo"
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
			if app.watch != nil {
				fn(app.watch)
			}
		}
	}
	name := "红果短剧"
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: name, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: "关于" + name},
			{Label: "检查更新…", Click: run(func() { go checkForUpdates(true) })},
			mygo.Separator(),
			{Role: mygo.RoleServices, Label: "服务"},
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: "隐藏" + name},
			{Role: mygo.RoleHideOthers, Label: "隐藏其他"},
			{Role: mygo.RoleUnhide, Label: "全部显示"},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: "退出" + name},
		}},
		{Label: "编辑", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo, Label: "撤销"},
			{Role: mygo.RoleRedo, Label: "重做"},
			mygo.Separator(),
			{Role: mygo.RoleCut, Label: "剪切"},
			{Role: mygo.RoleCopy, Label: "拷贝"},
			{Role: mygo.RolePaste, Label: "粘贴"},
			{Role: mygo.RoleSelectAll, Label: "全选"},
			mygo.Separator(),
			{Label: "搜索", Accelerator: "CmdOrCtrl+F", Click: run(func() { app.go2(pageSearch); app.search.focus = true })},
		}},
		{Label: "播放", Submenu: []*mygo.MenuItem{
			{Label: "播放/暂停", Click: run(onWatch(func(w *Watching) { w.togglePlay() }))},
			{Label: "上一集", Accelerator: "CmdOrCtrl+Up", Click: run(onWatch(func(w *Watching) { w.step(-1) }))},
			{Label: "下一集", Accelerator: "CmdOrCtrl+Down", Click: run(onWatch(func(w *Watching) { w.step(1) }))},
			mygo.Separator(),
			{Label: "快进 10 秒", Accelerator: "CmdOrCtrl+Right", Click: run(onWatch(func(w *Watching) { w.seekBy(10) }))},
			{Label: "快退 10 秒", Accelerator: "CmdOrCtrl+Left", Click: run(onWatch(func(w *Watching) { w.seekBy(-10) }))},
			mygo.Separator(),
			{Label: "追剧/取消追剧", Accelerator: "CmdOrCtrl+D", Click: run(onWatch(func(w *Watching) { app.toggleFollow(w.show) }))},
			{Label: "返回", Accelerator: "CmdOrCtrl+[", Click: run(app.closeWatch)},
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
			w := app.watch
			if w == nil || w.player == nil {
				return
			}
			switch cmd {
			case mygo.MediaPlay:
				if !w.player.Playing() {
					w.togglePlay()
				}
			case mygo.MediaPause, mygo.MediaStop:
				if w.player.Playing() {
					w.togglePlay()
				}
			case mygo.MediaNext:
				w.step(1)
			case mygo.MediaPrevious:
				w.step(-1)
			case mygo.MediaSeek:
				w.player.Seek(pos.Seconds())
				w.syncNowPlaying()
			}
			app.win.Invalidate()
		})
	})
}

// nowPlaying shows the episode in Control Center.
func (w *Watching) nowPlaying() {
	if w.player == nil || w.detail == nil {
		return
	}
	info := mygo.NowPlayingInfo{
		Title:    w.show.Title,
		Artist:   "第" + itoa(w.index+1) + "集",
		Album:    "红果短剧",
		Duration: time.Duration(w.player.Duration() * float64(time.Second)),
		Position: time.Duration(w.player.Position() * float64(time.Second)),
		Rate:     w.app.lib.Speed,
		State:    mygo.PlaybackPlaying,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaSeek | mygo.MediaNext | mygo.MediaPrevious,
	}
	if img := w.app.posters.Image(w.show.Cover); img != nil {
		var buf bytes.Buffer
		if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}) == nil {
			info.Artwork = buf.Bytes()
		}
	}
	mygo.NowPlaying.Set(info)
}

// syncNowPlaying tells Control Center where playback is.
func (w *Watching) syncNowPlaying() {
	if w.player == nil {
		return
	}
	state := mygo.PlaybackPaused
	if w.player.Playing() {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, time.Duration(w.player.Position()*float64(time.Second)))
}

// checkForUpdates looks for a newer version and offers to install it.
// manual says the user asked, who then hears that there is none too.
func checkForUpdates(manual bool) {
	if !mygo.Updater.Enabled() {
		if manual {
			mygo.Dialog.Message(mygo.MessageOptions{Message: "此版本不支持自动更新", Detail: "开发版本或没有写入权限的位置不能自动更新。"})
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		if manual {
			msg, detail := "已是最新版本", "当前版本 "+mygo.App.Version()+" 是最新的。"
			if err != nil && !errors.Is(err, context.Canceled) {
				msg, detail = "检查更新失败", err.Error()
			}
			mygo.Dialog.Message(mygo.MessageOptions{Message: msg, Detail: detail})
		}
		return
	}
	res, _ := mygo.Dialog.Message(mygo.MessageOptions{
		Message: "发现新版本 " + up.Version,
		Detail:  up.Notes,
		Buttons: []string{"安装并重启", "以后再说"},
	})
	if res.Button != 0 {
		return
	}
	ictx, icancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer icancel()
	if err := up.Install(ictx, nil); err != nil {
		mygo.Dialog.Message(mygo.MessageOptions{Message: "更新失败", Detail: err.Error()})
		return
	}
	mygo.App.Relaunch()
}
