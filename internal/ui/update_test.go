package ui

import (
	"os"
	"slices"
	"testing"
)

func TestParseChangelog(t *testing.T) {
	text := "# Changelog\n\nintro\n\n## Unreleased\n\n- next\n\n## 2026.1121.2\n\n- 改名\n- 两行的\n  说明\n\n## 0.2.0-beta.3\n\n修复了排行榜。\n"
	got := parseChangelog(text)
	if len(got) != 2 {
		t.Fatalf("got %d sections: %+v", len(got), got)
	}
	if got[0].version != "2026.1121.2" || !slices.Equal(got[0].Items, []string{"改名", "两行的 说明"}) {
		t.Errorf("first section: %+v", got[0])
	}
	if got[1].version != "0.2.0-beta.3" || !slices.Equal(got[1].Items, []string{"修复了排行榜。"}) {
		t.Errorf("second section: %+v", got[1])
	}
}

func TestParseOwnChangelog(t *testing.T) {
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Skip(err)
	}
	for _, n := range parseChangelog(string(b)) {
		if len(n.Items) == 0 {
			t.Errorf("%s has no items", n.version)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2026.1121.2", "2026.1121.1", 1},
		{"2026.1122.1", "2026.1121.9", 1},
		{"2026.1121.1", "0.2.0-beta.3", 1},
		{"0.2.0", "0.2.0-beta.3", 1},
		{"0.2.0-beta.3", "0.2.0-beta.10", -1},
		{"v2026.1121.2", "2026.1121.2", 0},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
