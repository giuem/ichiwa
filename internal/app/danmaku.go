package app

import (
	"context"
	"fmt"
	"log"

	"github.com/giuem/ichiwa/internal/source"
)

// Danmaku is the comments of an episode, as they are read. The view lays
// them out.
type Danmaku struct {
	Episode string           // whose comments these are
	Items   []source.Danmaku // in the order read
	Done    bool             // all read
	cancel  func()           // stops reading them
}

// danmakuPages caps the pages read for an episode, should the server keep
// saying there are more.
const danmakuPages = 200

// syncDanmaku reads the comments of the episode playing, page by page,
// once it plays and while they are wanted.
func (w *Watching) SyncDanmaku() {
	app := w.App
	if !w.Has(source.CapDanmaku) || !app.Lib.Danmaku || w.Detail == nil || w.Player == nil {
		return
	}
	ep := w.Detail.Episodes[w.Index]
	if w.DM != nil && w.DM.Episode == ep.ID {
		return
	}
	w.dropDanmaku()
	ctx, cancel := context.WithCancel(context.Background())
	d := &Danmaku{Episode: ep.ID, cancel: cancel}
	w.DM = d
	series := w.Show.ID
	src := w.Src
	go func() {
		defer cancel()
		cursor := ""
		for range danmakuPages {
			page, err := src.Danmaku(ctx, source.DanmakuRequest{
				MediaID:   series,
				EpisodeID: ep.ID,
				Cursor:    cursor,
			})
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("danmaku of %s: %v", ep.ID, err)
				return
			}
			last := page.Cursor == ""
			app.dispatch(func() {
				if w.DM == d {
					d.Items = append(d.Items, page.Items...)
					d.Done = last
				}
			})
			if last {
				return
			}
			cursor = page.Cursor
		}
	}()
}

// dropDanmaku stops reading the comments and forgets them.
func (w *Watching) dropDanmaku() {
	if w.DM != nil {
		w.DM.cancel()
		w.DM = nil
	}
}

func (w *Watching) ToggleDanmaku() {
	lib := w.App.Lib
	lib.Danmaku = !lib.Danmaku
	lib.MarkDirty()
	// Comments not all read, as when reading them failed, are read again
	// when turned back on.
	if !lib.Danmaku && w.DM != nil && !w.DM.Done {
		w.dropDanmaku()
	}
	w.App.Toast = ternary(lib.Danmaku, "已开启弹幕", "已关闭弹幕")
}

// nowPlaying tells the system the episode playing.
func (w *Watching) nowPlaying() {
	if w.Player == nil || w.Detail == nil || w.App.host == nil {
		return
	}
	w.App.host.NowPlaying(&NowPlaying{
		Title:    w.Show.Title,
		Artist:   fmt.Sprintf("第%d集", w.Index+1),
		Image:    w.Show.Image,
		Duration: w.Player.Duration(),
		Position: w.Player.Position(),
		Rate:     w.App.Lib.Speed,
	})
}

// SyncNowPlaying tells the system where playback is.
func (w *Watching) SyncNowPlaying() {
	if w.Player == nil || w.App.host == nil {
		return
	}
	w.App.host.Playback(w.Player.Playing(), w.Player.Position())
}
