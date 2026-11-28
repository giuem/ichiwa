package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/giuem/ichiwa/internal/app"
	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source/sourcetest"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/source"
)

func a11yApp(t *testing.T) *App {
	t.Helper()
	return viewApp(t, darkPalette())
}

func accessOf(t *testing.T, tt *ui.Tester, r ui.Role, name string) ui.AccessInfo {
	t.Helper()
	a, ok := tt.AccessByRole(r, name)
	if !ok {
		t.Fatalf("no %v named %q in the accessibility tree", r, name)
	}
	return a
}

// A card is a button the keyboard reaches and that says more than its
// title, and the control in its corner is a button of its own beside it.
func TestCardsAreButtons(t *testing.T) {
	app := a11yApp(t)
	s := Card{Kind: source.CardMedia, ID: "a", Title: "长安", Badges: []string{"全80集"}}
	app.Lib.Progress("hongguo", s, "e2", 2, 80, 30, 100)
	tt := ui.NewTester(app.historyPage, 900, 600)
	tt.Frame()
	card := accessOf(t, tt, ui.RoleButton, "长安")
	if !strings.Contains(card.Description, "看到第3集") {
		t.Errorf("the card says %q", card.Description)
	}
	forget := accessOf(t, tt, ui.RoleButton, "移除“长安”的记录")
	if forget.Bounds.X < card.Bounds.X || forget.Bounds.Y+forget.Bounds.H > card.Bounds.Y+card.Bounds.H+1 {
		t.Errorf("the corner control %+v is outside the card %+v", forget.Bounds, card.Bounds)
	}
	// The filters are one stop of Tab, then the card.
	for range 2 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !tt.Focused("长安") {
		t.Error("Tab doesn't reach the card")
	}
	if err := tt.Click("移除“长安”的记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.Lib.Recent()) != 0 {
		t.Error("the corner control didn't take the click")
	}
	if app.Watch != nil {
		t.Error("the corner control opened the card")
	}
}

// The player's controls say what they will do and what they are at.
func TestPlayerControlsSpeak(t *testing.T) {
	app := a11yApp(t)
	app.Lib.OnTop, app.Lib.Volume, app.Lib.Speed = true, 0.5, 1.5
	w := &Watching{App: app.App, Panel: true, Index: 1, Detail: &Media{Episodes: make([]source.Episode, 3)}}
	tt := ui.NewTester(func(c *ui.Context) { app.stage(c, w, false, false) }, 1200, 600) // room for the volume slider beside the panel
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleButton, "播放"); a.Description != "" || a.KeyShortcuts == "" {
		t.Errorf("the play button: %+v", a)
	}
	if a := accessOf(t, tt, ui.RoleToggleButton, "窗口置顶"); !a.Checked {
		t.Error("the pin doesn't say the window is pinned")
	}
	if a := accessOf(t, tt, ui.RoleButton, "选集面板"); !a.Expanded {
		t.Error("the panel button doesn't say the panel shows")
	}
	if a := accessOf(t, tt, ui.RoleButton, "播放速度"); a.Value != "1.5倍" {
		t.Errorf("the speed reads %q", a.Value)
	}
	if a := accessOf(t, tt, ui.RoleButton, "音量"); a.Value != "50%" || a.Expanded {
		t.Errorf("the volume button: %+v", a)
	}
	if a := accessOf(t, tt, ui.RoleHeading, ""); a.Level != 1 {
		t.Errorf("the title's level is %d", a.Level)
	}
	if err := tt.Click("音量"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleSlider, "音量"); a.Value != "50%" {
		t.Errorf("the volume reads %q", a.Value)
	}
}

// Filters are radio groups, with the row's all-option saying what it does.
func TestFilterOptionsAreRadios(t *testing.T) {
	app := a11yApp(t)
	cs := viewApp(t, darkPalette()).Col("filter")
	cs.Rows = []FilterRow{{
		Key:   "k",
		Title: "题材",
		Options: []source.FilterOption{
			{Value: "", Title: "题材"},
			{Value: "v", Title: "甜宠"},
		},
		Value: "v",
	}}
	cs.Chosen = map[string]string{"k": "v"}
	tt := ui.NewTester(func(c *ui.Context) { app.filterRows(c, cs) }, 900, 600)
	tt.Frame()
	accessOf(t, tt, ui.RoleRadioGroup, "题材")
	if a := accessOf(t, tt, ui.RoleRadio, "甜宠"); !a.Checked {
		t.Error("the chosen option isn't checked")
	}
	if a := accessOf(t, tt, ui.RoleRadio, "全部题材"); a.Checked {
		t.Error("the all-option is checked with a value chosen")
	}
}

// The sidebar says which page shows, and page titles are headings.
func TestNavigationSaysCurrentPage(t *testing.T) {
	app := a11yApp(t)
	app.Page = "rank"
	tt := ui.NewTester(func(c *ui.Context) {
		app.navItem(c, iconHome, "推荐", "channel:推荐", 0)
		app.navItem(c, iconTrophy, "排行榜", "rank", 3)
		app.header(c, "排行榜", "", nil)
	}, 900, 600)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleButton, "排行榜"); a.Current != ui.CurrentPage || a.Description != "3部" {
		t.Errorf("the current page's item: %+v", a)
	}
	if a := accessOf(t, tt, ui.RoleButton, "推荐"); a.Current != ui.CurrentNone {
		t.Error("another page's item says it is current")
	}
	if a := accessOf(t, tt, ui.RoleHeading, "排行榜"); a.Level != 1 {
		t.Errorf("the title's level is %d", a.Level)
	}
}

// More contrast raises secondary text and leaves the main colors.
func TestContrastedPalette(t *testing.T) {
	p, h := lightPalette(), lightPalette().contrasted()
	if h.text != p.text || h.accent != p.accent {
		t.Error("the main colors changed")
	}
	if h.muted == p.muted || h.faint == p.faint || h.hairline == p.hairline {
		t.Error("the secondary colors didn't")
	}
}

// The history filters show the dramas being watched or those watched to
// the end, and each card says which.
func TestHistoryFilter(t *testing.T) {
	app := a11yApp(t)
	app.Lib.Progress("hongguo", Card{Kind: source.CardMedia, ID: "a", Title: "长安"}, "e1", 1, 3, 30, 100)
	app.Lib.Progress("hongguo", Card{Kind: source.CardMedia, ID: "b", Title: "洛阳"}, "e2", 2, 3, 100, 100)
	tt := ui.NewTester(app.historyPage, 900, 600)
	tt.Frame()
	if err := tt.Click("看完"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "长安"); ok {
		t.Error("a drama being watched shows under 看完")
	}
	if a := accessOf(t, tt, ui.RoleButton, "洛阳"); !strings.Contains(a.Description, "已看完") {
		t.Errorf("the card says %q", a.Description)
	}
}

// The episodes say which were watched to the end and how far others got.
func TestEpisodesSayProgress(t *testing.T) {
	app := a11yApp(t)
	s := Card{Kind: source.CardMedia, ID: "a", Title: "长安"}
	app.Lib.Progress("hongguo", s, "e0", 0, 3, 60, 60)
	app.Lib.Progress("hongguo", s, "e1", 1, 3, 15, 60)
	eps := []source.Episode{{ID: "e0"}, {ID: "e1"}, {ID: "e2"}}
	w := &Watching{App: app.App, SourceID: "hongguo", Show: s, Index: 2, Detail: &Media{ID: "a", Title: "长安", Episodes: eps}}
	tt := ui.NewTester(func(c *ui.Context) { app.episodes(c, w) }, 400, 400)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleButton, "第1集"); a.Description != "已看完" {
		t.Errorf("episode 1 says %q", a.Description)
	}
	if a := accessOf(t, tt, ui.RoleButton, "第2集"); a.Description != "看到25%" {
		t.Errorf("episode 2 says %q", a.Description)
	}
}

// viewApp is an app without a window or sources, drawn in pal.
func viewApp(t *testing.T, pal palette, srcs ...source.Source) *App {
	t.Helper()
	return &App{App: core.Headless(testLibrary(t), func(func()) {}, srcs...), pal: pal, posters: newPosters(t.TempDir(), func() {})}
}

// The pages of the sources say what they are: the switcher is a menu
// button that says what it does, the sources page's parts are headings
// under its title, each source's switch is a switch named for it, and the
// search suggestions are a list of buttons.
func TestSourcePagesSpeak(t *testing.T) {
	src := &sourcetest.Source{
		InfoData: source.Info{ID: "a", Name: "甲片源", Capabilities: []source.Capability{source.CapSearch, source.CapSearchSuggest}},
		OnNavigation: func(context.Context) ([]source.NavItem, error) {
			return []source.NavItem{{ID: "home", Title: "首页"}}, nil
		},
	}
	app := viewApp(t, darkPalette().contrasted(), src)
	tt := ui.NewTester(app.sidebar, 220, 600)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleMenuButton, "片源：甲片源"); a.Description != "切换片源" {
		t.Errorf("the switcher: %+v", a)
	}

	app.Go(core.PageSources)
	tt = ui.NewTester(app.content, 1000, 700)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleHeading, "片源"); a.Level != 1 {
		t.Errorf("the title's level is %d", a.Level)
	}
	for _, h := range []string{"已安装", "添加片源", "开发者模式"} {
		if a := accessOf(t, tt, ui.RoleHeading, h); a.Level != 2 {
			t.Errorf("%s's level is %d", h, a.Level)
		}
	}
	if a := accessOf(t, tt, ui.RoleSwitch, "打开甲片源"); !a.Checked {
		t.Error("the switch of a source on is off")
	}

	app.Go(core.PageSearch)
	app.Search.Suggests, app.Search.SuggestFor, app.Search.Query = []string{"兔子", "兔子二"}, "兔", "兔"
	tt = ui.NewTester(app.content, 1000, 700)
	tt.Frame()
	accessOf(t, tt, ui.RoleList, "搜索建议")
	if err := tt.Click("兔子二"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.Search.Shown != "兔子二" {
		t.Fatalf("searched %q", app.Search.Shown)
	}
}

// The pages new with the sources speak: the guide, a plugin's log, the
// dialog of a record that can't open, an actor's header and the cast.
func TestNewPagesSpeak(t *testing.T) {
	app := viewApp(t, darkPalette().contrasted())
	tt := ui.NewTester(app.content, 1000, 700)
	tt.UseScreenReader()
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleHeading, "还没有片源"); a.Level != 1 {
		t.Errorf("the guide's title is at level %d", a.Level)
	}
	accessOf(t, tt, ui.RoleButton, "管理片源")

	p, err := plugin.Open(filepath.Join("..", "plugin", "testdata", "example"), plugin.Options{})
	if err != nil {
		t.Fatal(err)
	}
	app = viewApp(t, darkPalette().contrasted(), p)
	app.Go(core.PageSources)
	app.LogOf = "example"
	tt = ui.NewTester(app.content, 1000, 700)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleHeading, "示例片源的日志"); a.Level != 1 {
		t.Errorf("the log's title is at level %d", a.Level)
	}
	accessOf(t, tt, ui.RoleButton, "返回")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.LogOf != "" {
		t.Error("Esc left the log open")
	}

	app.Unavailable = &core.Unavailable{Source: "gone", Name: "gone", Title: "丙剧"}
	tt = ui.NewTester(app.view, 1000, 700)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleDialog, "片源不可用"); a.Name == "" {
		t.Error("the dialog has no name")
	}
	accessOf(t, tt, ui.RoleButton, "好")
	accessOf(t, tt, ui.RoleButton, "管理片源")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.Unavailable != nil {
		t.Error("Esc left the dialog open")
	}

	app.OpenCollection(Card{Kind: source.CardCollection, ID: "person:1", Title: "李柯以", Image: "x", Shape: source.ShapeCircle})
	cs := app.Stack[0]
	cs.First, cs.Header = &source.CollectionPage{}, &source.Header{Title: "李柯以", Meta: []source.Meta{{Value: "女演员"}}, Description: "演员简介"}
	cs.Results.Loaded, cs.Results.More = true, false
	tt = ui.NewTester(app.content, 1000, 700)
	tt.Frame()
	if a := accessOf(t, tt, ui.RoleHeading, "李柯以"); a.Level != 1 {
		t.Errorf("the actor's name is at level %d", a.Level)
	}
	for _, s := range []string{"女演员", "演员简介"} {
		if _, ok := tt.Access(s); !ok {
			t.Errorf("screen readers don't read %q", s)
		}
	}

	w := &Watching{App: app.App, SourceID: "example", Show: Card{Kind: source.CardMedia, ID: "s1"},
		Detail: &Media{People: []Card{{Kind: source.CardCollection, ID: "person:1", Title: "张三", Subtitle: "饰 李四", Shape: source.ShapeCircle}}}}
	tt = ui.NewTester(func(c *ui.Context) { app.related(c, w) }, 368, 300)
	tt.Frame()
	accessOf(t, tt, ui.RoleHeading, "演职人员")
	if a := accessOf(t, tt, ui.RoleButton, "张三"); a.Description != "饰 李四" || !a.Focusable {
		t.Errorf("the cast member: %+v", a)
	}
}
