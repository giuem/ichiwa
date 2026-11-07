package app

import (
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
	"github.com/egoist/mygo/ui"

	"hongguo/internal/library"
)

// Times show as minutes and seconds, and hours only past an hour.
func TestFmtTime(t *testing.T) {
	for in, want := range map[float64]string{-3: "00:00", 0: "00:00", 59.6: "01:00", 145.4: "02:25", 3725: "1:02:05"} {
		if got := fmtTime(in); got != want {
			t.Errorf("fmtTime(%v) = %q, want %q", in, got, want)
		}
	}
}

// The grid fits as many cards as it can, never fewer than two, and fills
// the width exactly.
func TestColumns(t *testing.T) {
	for _, w := range []float32{200, 700, 1036, 1800} {
		n, cw := columns(w)
		if n < 2 || cw < cardMinW && n > 2 {
			t.Errorf("columns(%v) = %d of %v", w, n, cw)
		}
		if got := float32(n)*cw + float32(n-1)*cardGap; got < w-0.5 || got > w+0.5 {
			t.Errorf("columns(%v) fills %v", w, got)
		}
	}
}

// The player's buttons take their clicks: the picture, laid over the whole
// stage, once covered them and took every click as play/pause.
func TestStageButtonsTakeClicks(t *testing.T) {
	app := &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: library.Load(t.TempDir())}
	w := &Watching{app: app, panel: true, index: 1,
		detail: &Detail{Episodes: make([]model.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 900, 600)
	tt.Frame()
	if err := tt.Click("选集面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.panel {
		t.Fatal("the panel button didn't take the click")
	}
	if w.paused {
		t.Fatal("the click went to the picture")
	}
}
