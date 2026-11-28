// Package app is the state of the app and how it goes: the source shown,
// its pages and their loads, the player, the records. It draws nothing;
// internal/ui draws it and takes the user's actions to it, and Host is
// what it needs of the window.
package app

import (
	"context"
	"log"
	"time"

	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/media"
	"github.com/giuem/ichiwa/internal/source"
	"github.com/giuem/ichiwa/internal/sources/hongguo"
)

// The pages of the sidebar that are not a source's navigation items.
const (
	PageSearch  = "search"
	PageHistory = "history"
	PageFollows = "follows"
)

// Host is what the app needs of the window it shows in.
type Host interface {
	// Post runs fn on the UI thread, later, and redraws the window. It
	// may be called from any goroutine.
	Post(fn func())
	// Sync runs fn on the UI thread and waits for it; it is called
	// from other goroutines only.
	Sync(fn func())
	// Redraw redraws the window; from any goroutine.
	Redraw()
	// OpenVideo opens a player for a URL, or a file when url is "".
	OpenVideo(url, path string) (Player, error)
	// KeepAwake keeps the display on until release is called.
	KeepAwake(reason string) (release func())
	// NowPlaying tells the system what plays, nil for nothing.
	NowPlaying(n *NowPlaying)
	// Playback tells the system where playback is.
	Playback(playing bool, at time.Duration)
	// SetAlwaysOnTop keeps the window above others, or not.
	SetAlwaysOnTop(on bool)
}

// Player plays an episode.
type Player interface {
	Play()
	Pause()
	Playing() bool
	Ended() bool
	Ready() bool
	Err() error
	Duration() time.Duration
	Position() time.Duration
	Seek(at time.Duration)
	SetRate(rate float64)
	SetVolume(v float64)
	// Size is the size of the video, 0 until known.
	Size() (w, h int)
	Close()
}

// NowPlaying is what the system shows of the episode playing.
type NowPlaying struct {
	Title, Artist string
	Image         string // the URL of the cover
	Duration      time.Duration
	Position      time.Duration
	Rate          float64
}

// Config is where the app keeps its data and what it tells plugins.
type Config struct {
	DataDir  string // what the app must not lose
	CacheDir string // what it may lose: covers, episodes, lists
	Version  string // of the app, "" in development
	Locale   string // the user's language
	Host     Host
}

// App is the state the window shows.
type App struct {
	host    Host
	version string
	Src     source.Source // the source shown
	Sources registry      // every source loaded
	// srcCtx is canceled when the app leaves the source shown, pageCtx
	// when it leaves the page shown (§9): a call they canceled is dropped
	// without an error. Both are made when first asked for.
	srcCtx, pageCtx   context.Context
	srcStop, pageStop context.CancelFunc
	// locale is the user's language, for plugins; "" in tests.
	locale  string
	Dev     devState
	Media   *media.Manager
	Nav     []source.NavItem
	NavErr  string
	NavBusy bool
	// cols are the collection pages of the navigation, by collection ID.
	cols     map[string]*Collection
	DataDir  string
	CacheDir string
	Lib      *library.Library
	InitErr  string

	// Page is the page of the sidebar shown: a navigation item of the
	// source shown, PageSearch, or one of the app's own pages.
	Page   string
	Search searchState
	// srcPage is the page of the source shown last, which it goes back
	// to from the app's own pages.
	srcPage string
	// kept is what the app showed of the sources it left, by ID, so that
	// each shows again where the user left it.
	kept map[string]*browse

	// Stack is the collections opened from cards, over the sidebar's
	// page, the last on top.
	Stack []*Collection

	// Watch is the player page, over the rest while it is open.
	Watch *Watching
	// LogOf is the source whose log the sources page shows, "" for none.
	LogOf string
	// Unavailable is a record of a source not available that the user
	// opened, to tell about; nil when there is none.
	Unavailable *Unavailable
	// Toast is a message to show once.
	Toast string
	// unmute is the volume the mute button goes back to.
	unmute float64
	// MiniOn asks for the small window.
	MiniOn bool

	// The sleep timer, which outlasts a season going on to the next:
	// SleepMins is the choice, SleepEpisode for the end of this episode,
	// and SleepAt when the time runs out.
	SleepMins int
	SleepAt   time.Time

	warmed bool // the first pages of the other pages were asked for

	// post schedules callbacks onto the UI thread in tests; host.Post
	// otherwise.
	post func(func())
}

// New loads what the app keeps and the sources, and shows the source
// shown last.
func New(cfg Config) *App {
	app := &App{host: cfg.Host, version: cfg.Version, locale: cfg.Locale, DataDir: cfg.DataDir, CacheDir: cfg.CacheDir}
	app.Lib = library.Load(cfg.DataDir)
	app.Page = normalizeSavedPage(app.Lib.Page)
	src, err := hongguo.New(cfg.DataDir)
	if err != nil {
		app.InitErr = err.Error()
		return app
	}
	app.Sources.Add(src)
	mgr, err := media.New(cfg.CacheDir)
	if err != nil {
		app.InitErr = err.Error()
		return app
	}
	app.Media = mgr
	mgr.SetPolicy(app.Sources.mediaPolicy)
	// The developer mode's plugin folder, loaded at the last launch.
	shown := app.Lib.Source
	if dev := app.loadDevSettings(); dev.Dir != "" {
		if err := app.LoadDev(dev.Dir); err != nil {
			log.Printf("plugins: %s: %v", dev.Dir, err)
		} else if dev.Env || shown == "" && dev.Current != "" {
			shown = app.Dev.Plugin.Info().ID
			delete(app.Lib.Off, shown)
		}
	}
	s := app.Sources.Get(shown)
	if s == nil || !app.SourceOn(shown) {
		s = app.firstOn()
	}
	saved := app.Page
	app.UseSource(s)
	if s != nil && s.Info().ID == shown {
		app.srcPage = saved // where the user left it
	}
	if appPage(saved) {
		app.Page = saved
	}
	return app
}

func (app *App) dispatch(fn func()) {
	if app.post != nil {
		app.post(fn)
		return
	}
	if app.host != nil {
		app.host.Post(fn)
	}
}

// sync runs fn on the UI thread and waits for it.
func (app *App) sync(fn func()) {
	switch {
	case app.post != nil:
		done := make(chan struct{})
		app.post(func() { fn(); close(done) })
		<-done
	case app.host != nil:
		app.host.Sync(fn)
	default:
		fn()
	}
}

// redraw redraws the window.
func (app *App) redraw() {
	if app.host != nil {
		app.host.Redraw()
	}
}

// Flush saves what changed: progress, settings and the lists kept.
func (app *App) Flush(wait bool) {
	app.Lib.Flush()
	app.SaveFeeds(wait)
}

func (app *App) SourceID() string {
	if app.Src != nil {
		return app.Src.Info().ID
	}
	return library.LegacySource
}

func (app *App) HasCap(c source.Capability) bool {
	return app.Src != nil && app.Src.Info().Has(c)
}

// async runs work on a goroutine and then done with its result on the main
// thread, redrawing the window.
func async[T any](app *App, work func() (T, error), done func(T, error)) {
	go func() {
		v, err := work()
		app.dispatch(func() { done(v, err) })
	}()
}

// srcContext is the context of the calls for the source shown.
func (app *App) srcContext() context.Context {
	if app.srcCtx == nil {
		app.srcCtx, app.srcStop = context.WithCancel(context.Background())
	}
	return app.srcCtx
}

// pageContext is the context of the calls for the page shown.
func (app *App) pageContext() context.Context {
	if app.pageCtx == nil {
		app.pageCtx, app.pageStop = context.WithCancel(app.srcContext())
	}
	return app.pageCtx
}

// leavePage cancels the calls for the page shown.
func (app *App) leavePage() {
	if app.pageStop != nil {
		app.pageStop()
	}
	app.pageCtx, app.pageStop = nil, nil
}

// leaveSource cancels the calls for the source shown and its pages.
func (app *App) leaveSource() {
	app.leavePage()
	if app.srcStop != nil {
		app.srcStop()
	}
	app.srcCtx, app.srcStop = nil, nil
}

// go2 shows a page of the sidebar.
func (app *App) Go(page string) {
	if page != app.Page || len(app.Stack) > 0 {
		app.leavePage()
	}
	app.Page = page
	app.LogOf = ""
	if !appPage(page) {
		app.srcPage = page
	}
	for len(app.Stack) > 0 {
		app.CloseCollection()
	}
	app.Lib.Page = page
	app.Lib.MarkDirty()
	if app.Watch != nil {
		app.CloseWatch()
	}
}

// The smallest the window may be: the width takes the player's controls,
// the height the whole sidebar.
const (
	MinWindowWidth  = 480
	MinWindowHeight = 480
)

// normalizeSavedPage is the page saved by an earlier version as this
// version names it: the first ones named Hongguo's channels by title.
func normalizeSavedPage(saved string) string {
	switch saved {
	case "推荐", "真人剧", "漫剧":
		return "channel:" + saved
	}
	return saved
}

// Headless is an app without a window, for the tests of the views: lib
// keeps its records, post runs what the loads bring, and the first of
// srcs shows.
func Headless(lib *library.Library, post func(func()), srcs ...source.Source) *App {
	app := &App{Lib: lib, post: post, warmed: true}
	for _, s := range srcs {
		app.Sources.Add(s)
	}
	if len(srcs) > 0 {
		app.Src = srcs[0]
	}
	return app
}
