package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// updateInterval is how often the app looks for a newer version while it
// runs, after the first look at launch.
const updateInterval = 6 * time.Hour

// changelogURL is CHANGELOG.md as the release tagged v<version> has it,
// for the notes of every version since the running one.
const changelogURL = "https://github.com/giuem/ichiwa/raw/refs/tags/v%s/CHANGELOG.md"

// updateState is a newer version found and its dialog.
type updateState struct {
	up    *mygo.Update // nil while the app is up to date or has not looked
	notes []releaseNotes
	Open  bool // the dialog shows

	installing  bool
	done, total int64 // bytes downloaded, of total
	Err         string
}

// releaseNotes are the changes of a version, from its section of the
// changelog.
type releaseNotes struct {
	version string
	Items   []string
}

// watchUpdates looks for a newer version soon after launch and then every
// updateInterval, quietly: a newer one only shows as a mark by the version
// in the sidebar, which opens the update dialog.
func (app *App) watchUpdates() {
	time.Sleep(5 * time.Second)
	for {
		app.checkForUpdates(false)
		time.Sleep(updateInterval)
	}
}

// checkForUpdates looks for a newer version and keeps it, with the notes
// of the versions since the running one, for the sidebar. manual says the
// user asked, who then sees the update dialog, or hears that there is no
// update or that the check failed.
func (app *App) checkForUpdates(manual bool) {
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
	notes := changesSince(ctx, mygo.App.Version(), up)
	app.win.Update(func() {
		u := &app.upd
		if u.installing {
			return
		}
		if u.up == nil || u.up.Version != up.Version {
			u.up, u.notes, u.Err = up, notes, ""
		}
		if manual {
			u.Open = true
		}
	})
}

// changesSince returns the notes of the versions after current up to up's,
// newest first, from the changelog of up's release; or up's own notes when
// the changelog cannot be had.
func changesSince(ctx context.Context, current string, up *mygo.Update) []releaseNotes {
	if text, err := fetchChangelog(ctx, up.Version); err == nil {
		var out []releaseNotes
		for _, n := range parseChangelog(text) {
			if compareVersions(n.version, current) > 0 && compareVersions(n.version, up.Version) <= 0 {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []releaseNotes{{version: up.Version, Items: noteItems(up.Notes)}}
}

func fetchChangelog(ctx context.Context, version string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(changelogURL, version), nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("changelog: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return string(b), err
}

// parseChangelog returns the versioned sections of a changelog, in its
// order; sections whose heading is not a version, as Unreleased, are left
// out.
func parseChangelog(text string) []releaseNotes {
	var out []releaseNotes
	var body []string
	version := ""
	flush := func() {
		if version != "" {
			out = append(out, releaseNotes{version: version, Items: noteItems(strings.Join(body, "\n"))})
		}
		body = body[:0]
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			version = ""
			if f := strings.Fields(rest); len(f) > 0 && isVersion(f[0]) {
				version = strings.TrimPrefix(f[0], "v")
			}
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

// noteItems splits the notes of a version into its items: the lines of a
// list, each with the lines that continue it, or the paragraphs.
func noteItems(notes string) []string {
	var items []string
	cont := false
	for _, line := range strings.Split(notes, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			cont = false
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			items = append(items, strings.TrimSpace(t[2:]))
			cont = true
		case cont:
			items[len(items)-1] += " " + t
		default:
			items = append(items, t)
			cont = true
		}
	}
	return items
}

func isVersion(s string) bool {
	s = strings.TrimPrefix(s, "v")
	core, _, _ := strings.Cut(s, "-")
	for _, f := range strings.Split(core, ".") {
		if _, err := strconv.Atoi(f); err != nil {
			return false
		}
	}
	return true
}

// compareVersions orders versions as the updater does: the dotted numbers,
// then a version with a prerelease (1.0.0-beta.1) before the one without.
func compareVersions(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	ac, apre, _ := strings.Cut(a, "-")
	bc, bpre, _ := strings.Cut(b, "-")
	if c := compareFields(ac, bc); c != 0 {
		return c
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return compareFields(apre, bpre)
}

func compareFields(a, b string) int {
	af, bf := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(af) || i < len(bf); i++ {
		var x, y string
		if i < len(af) {
			x = af[i]
		}
		if i < len(bf) {
			y = bf[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				return cmpInt(xn, yn)
			}
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// installUpdate downloads and installs the update the dialog shows, and
// relaunches into it.
func (app *App) installUpdate() {
	u := &app.upd
	up := u.up
	u.installing, u.done, u.total, u.Err = true, 0, 0, ""
	go func() {
		var last atomic.Int64
		progress := func(done, total int64) {
			// Redraws at most ten times a second.
			now := time.Now().UnixMilli()
			if now-last.Load() < 100 && done < total {
				return
			}
			last.Store(now)
			app.win.Update(func() { u.done, u.total = done, total })
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := up.Install(ctx, progress); err != nil {
			app.win.Update(func() { u.installing, u.Err = false, "更新失败："+err.Error() })
			return
		}
		mygo.App.Relaunch()
	}()
}

// updateDialog shows the version found, the changes since the running
// one, and installs it.
func (app *App) updateDialog(c *ui.Context) {
	u := &app.upd
	if u.up == nil {
		u.Open = false
		return
	}
	p := app.pal
	d := ui.DialogBase(c, &u.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.Width(440).Radius(16).Background(p.bg).Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.3)).
			Label("发现新版本 " + u.up.Version)
		if p.dark {
			panel.Border(1, p.hairline)
		}
		ui.Row(c).Padding(24, 24, 18).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Image(c, logo).Size(56, 56).Margin(-4, -4, -4, -4)
			ui.Column(c).Gap(5).Grow(1).Children(func() {
				ui.Text(c, "一集有新版本了").FontSize(17).Bold().TextColor(p.text).Role(ui.RoleHeading).Level(2)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, mygo.App.Version()).FontSize(12.5).TextColor(p.faint)
					ui.Text(c, "→").FontSize(12.5).TextColor(p.faint)
					ui.Text(c, u.up.Version).FontSize(12.5).FontWeight(600).TextColor(p.accent)
				})
			})
		})
		ui.Box(c).Height(1).Background(p.hairline)
		ui.Scroll(c.Key("update-notes")).MaxHeight(300).Children(func() {
			ui.Column(c).Padding(16, 24, 18).Gap(16).Children(func() {
				ui.Text(c, "更新内容").FontSize(12).FontWeight(600).TextColor(p.muted)
				for i, n := range u.notes {
					ui.Column(c.Key(n.version)).Gap(8).Children(func() {
						if len(u.notes) > 1 {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Text(c, n.version).FontSize(12.5).FontWeight(600).TextColor(p.text)
								if i == 0 {
									ui.Text(c, "最新").FontSize(10.5).FontWeight(600).TextColor(p.accent).
										Padding(1, 6).Radius(999).Background(p.accent.Alpha(0.12))
								}
							})
						}
						if len(n.Items) == 0 {
							ui.Text(c, "修复与改进").FontSize(13.5).TextColor(p.muted)
						}
						for _, item := range n.Items {
							ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
								ui.Box(c).Size(5, 5).Radius(999).Background(p.accent).Margin(8, 0, 0).Shrink(0)
								ui.Text(c, item).FontSize(13.5).LineHeight(1.5).TextColor(p.text).Grow(1).Shrink(1)
							})
						}
					})
				}
			})
		})
		ui.Box(c).Height(1).Background(p.hairline)
		ui.Column(c).Padding(16, 24, 20).Gap(12).Children(func() {
			if u.installing {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					label := "正在准备…"
					if u.total > 0 {
						label = "正在下载更新…"
						if u.done >= u.total {
							label = "正在安装…"
						}
					}
					ui.Text(c, label).FontSize(12.5).TextColor(p.muted).Grow(1)
					if u.total > 0 {
						ui.Text(c, fmt.Sprintf("%s / %s", megabytes(u.done), megabytes(u.total))).FontSize(12).TextColor(p.faint)
					}
				})
				frac := -1.0
				if u.total > 0 {
					frac = float64(u.done) / float64(u.total)
				}
				ui.Progress(c, frac).Label("下载进度")
				return
			}
			if u.Err != "" {
				ui.Text(c, u.Err).FontSize(12.5).TextColor(p.accent).Role(ui.RoleAlert)
			}
			ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
				app.secondary(c, nil, "以后再说").OnClick(func() { u.Open = false })
				label := "安装并重启"
				if u.Err != "" {
					label = "重试"
				}
				app.primary(c, nil, label).AutoFocus().OnClick(app.installUpdate)
			})
		})
	})
	// It stays open while the update installs.
	if d.Dismissed() && u.installing {
		u.Open = true
	}
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
