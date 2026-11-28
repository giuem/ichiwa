package app

import (
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/source/sourcetest"
)

// testHost runs what the app posts when run is called.
type testHost struct{ done chan func() }

func (h testHost) Post(fn func())                           { h.done <- fn }
func (h testHost) Sync(fn func())                           { c := make(chan struct{}); h.done <- func() { fn(); close(c) }; <-c }
func (h testHost) Redraw()                                  {}
func (h testHost) OpenVideo(string, string) (Player, error) { return nil, nil }
func (h testHost) KeepAwake(string) func()                  { return func() {} }
func (h testHost) NowPlaying(*NowPlaying)                   {}
func (h testHost) Playback(bool, time.Duration)             {}
func (h testHost) SetAlwaysOnTop(bool)                      {}
func (h testHost) run(d time.Duration) bool {
	select {
	case fn := <-h.done:
		fn()
		return true
	case <-time.After(d):
		return false
	}
}

// launch starts the app on the data in dir, until its navigation is in.
func launch(t *testing.T, dir string) *App {
	t.Helper()
	h := testHost{done: make(chan func(), 64)}
	app := New(Config{DataDir: dir, CacheDir: t.TempDir(), Host: h})
	if app.InitErr != "" {
		t.Fatal(app.InitErr)
	}
	t.Cleanup(func() {
		app.leaveSource()
		app.Lib.Close()
		// Windows removes no file still open.
		for _, s := range app.Sources.All() {
			if c, ok := s.(interface{ Close() error }); ok {
				c.Close()
			}
		}
	})
	for app.NavBusy && h.run(5*time.Second) {
	}
	return app
}

// The page the current release saved, without a source, shows again
// after the update: its old names too.
func TestUpgradeKeepsPage(t *testing.T) {
	for saved, want := range map[string]string{
		"rank":    "rank",
		"filter":  "filter",
		"漫剧":      "channel:漫剧",
		"search":  PageSearch,
		"history": PageHistory,
	} {
		dir := t.TempDir()
		lib := library.Load(dir)
		lib.Page = saved
		lib.MarkDirty()
		lib.Close()

		app := launch(t, dir)
		if app.Page != want || app.SourceID() != "hongguo" {
			t.Errorf("saved %q: shows %q of %q, want %q", saved, app.Page, app.SourceID(), want)
		}
		if appPage(want) && app.srcPage == want {
			t.Errorf("saved %q: the source's page is the app's", saved)
		}
	}
}

// After switching sources, the app starts again on the source and the
// page shown: switching from an app's page too.
func TestSwitchSourceSavesPage(t *testing.T) {
	a, b := navSource("a", "a1", "a2"), navSource("b", "b1", "b2")
	app, run := cancelApp(t, a)
	app.Sources.Add(b)
	app.UseSource(a)
	run(5 * time.Second)
	app.SwitchSource(b)
	run(5 * time.Second)
	app.Go("b2")
	app.Go(PageHistory)

	for _, step := range []struct {
		to   *sourcetest.Source
		want string
	}{{a, "a1"}, {b, "b2"}} {
		app.SwitchSource(step.to)
		run(300 * time.Millisecond)
		if app.Lib.Page != step.want || app.Lib.Source != step.to.Info().ID {
			t.Fatalf("switched to %s: saved %q of %q", step.to.Info().ID, app.Lib.Page, app.Lib.Source)
		}

		again, run2 := cancelApp(t, a)
		again.Lib = app.Lib
		again.Sources.Add(b)
		again.start(app.Lib.Source)
		run2(5 * time.Second)
		if again.Src != step.to || again.Page != step.want {
			t.Errorf("starting again after switching to %s: %q of %v", step.to.Info().ID, again.Page, again.Src)
		}
		again.leaveSource()
	}
}
