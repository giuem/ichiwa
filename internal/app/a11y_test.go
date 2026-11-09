package app

import (
	"strings"
	"testing"

	"github.com/beilunyang/openhongguo/sdk/model"
	"github.com/egoist/mygo/ui"
)

func a11yApp(t *testing.T) *App {
	t.Helper()
	return &App{pal: darkPalette(), posters: newPosters(t.TempDir(), func() {}), lib: testLibrary(t), feeds: map[string]*Feed{}}
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
	s := Show{ID: "a", Title: "长安", Episodes: 80}
	app.lib.Progress(s, 2, 30, 100)
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
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !tt.Focused("长安") {
		t.Error("Tab doesn't reach the card")
	}
	if err := tt.Click("移除“长安”的记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(app.lib.Recent()) != 0 {
		t.Error("the corner control didn't take the click")
	}
	if app.watch != nil {
		t.Error("the corner control opened the card")
	}
}

// The player's controls say what they will do and what they are at.
func TestPlayerControlsSpeak(t *testing.T) {
	app := a11yApp(t)
	app.lib.OnTop, app.lib.Volume, app.lib.Speed = true, 0.5, 1.5
	w := &Watching{app: app, panel: true, index: 1, detail: &Detail{Episodes: make([]model.Episode, 3)}}
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
	if a := accessOf(t, tt, ui.RoleSlider, "音量"); a.Value != "50%" {
		t.Errorf("the volume reads %q", a.Value)
	}
	if a := accessOf(t, tt, ui.RoleHeading, ""); a.Level != 1 {
		t.Errorf("the title's level is %d", a.Level)
	}
}

// Filters are radio groups, with the row's all-option saying what it does.
func TestFilterOptionsAreRadios(t *testing.T) {
	app := a11yApp(t)
	app.filter.rowsOK = true
	app.filter.rows = []FilterRow{{Key: "k", Name: "题材", Options: []model.FilterOption{{Name: "甜宠", Value: "v"}}}}
	app.filter.chosen = map[string]string{"k": "v"}
	tt := ui.NewTester(app.filterPanel, 900, 600)
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
	app.page = pageRank
	tt := ui.NewTester(func(c *ui.Context) {
		app.navItem(c, iconHome, "推荐", pageRecommend, 0)
		app.navItem(c, iconTrophy, "排行榜", pageRank, 3)
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
