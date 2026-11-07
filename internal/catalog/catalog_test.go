package catalog

import (
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
)

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
