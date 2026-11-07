package main

import (
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
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

// Playback prefers the sharpest stream up to 1080p, and a larger one only
// when nothing else is offered.
func TestRankStream(t *testing.T) {
	s360 := model.Stream{Width: 640, Height: 360, Bitrate: 300000}
	s720 := model.Stream{Width: 720, Height: 1280, Bitrate: 1200000}
	s1080 := model.Stream{Width: 1080, Height: 1920, Bitrate: 2500000}
	s4k := model.Stream{Width: 2160, Height: 3840, Bitrate: 9000000}
	if !(rankStream(s1080) > rankStream(s720) && rankStream(s720) > rankStream(s360)) {
		t.Error("sharper streams up to 1080p should rank higher")
	}
	if rankStream(s4k) > rankStream(s360) {
		t.Error("a stream above 1080p should rank below the others")
	}
}

// Following a drama and following it again leaves it unfollowed, and the
// latest followed comes first.
func TestFollow(t *testing.T) {
	l := loadLibrary(t.TempDir())
	a, b := Show{ID: "a"}, Show{ID: "b"}
	l.ToggleFollow(a)
	l.ToggleFollow(b)
	if got := l.Followed(); len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("Followed = %v", got)
	}
	l.ToggleFollow(a)
	if l.Following("a") || len(l.Followed()) != 1 {
		t.Fatal("following again should stop following")
	}
	again := loadLibrary(l.path[:len(l.path)-len("/library.json")])
	if !again.Following("b") {
		t.Fatal("follows are not saved")
	}
}
