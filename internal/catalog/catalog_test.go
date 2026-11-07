package catalog

import (
	"fmt"
	"testing"

	"github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

// friendly messages keep their cause, so autoplay can tell an episode the
// anonymous device cannot play from a network failure.
func TestFriendlyKeepsCause(t *testing.T) {
	err := friendly(fmt.Errorf("resolve: %w", sdk.ErrAnonymousLimited))
	if got := ErrText(err); got != "服务端暂时限制了匿名播放，请稍后再试" {
		t.Fatalf("text = %q", got)
	}
	if !Unavailable(err) || !Limited(err) {
		t.Fatalf("lost the cause: %v", err)
	}
	if err := friendly(sdk.ErrTransport); Unavailable(err) || Limited(err) {
		t.Fatal("a transport error counted as limited")
	}
	if err := friendly(sdk.ErrAuthenticationRequired); !Unavailable(err) || Limited(err) {
		t.Fatal("a login-only episode misjudged")
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
