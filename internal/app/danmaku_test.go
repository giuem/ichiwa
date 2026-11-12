package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"time"

	"hongguo/internal/catalog"
	"hongguo/internal/library"
)

func fixedWidth([]ui.Span) float32 { return 100 }

// testStyle is the style of the default settings.
var testStyle = danmakuStyle{flight: 8 * time.Second, size: 17, area: 2, opacity: 1}

func at(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// Comments together take lanes of their own; one that starts after the one
// before came in whole shares its lane.
func TestDanmakuLanes(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{Text: "a", At: at(1)}, {Text: "b", At: at(1)}, {Text: "c", At: at(4)}})
	d.advance(at(5), 800, 600, testStyle, fixedWidth)
	if d.lanes[0] != 0 || d.lanes[1] != 1 || d.lanes[2] != 0 {
		t.Fatalf("lanes = %v", d.lanes)
	}
}

// With every lane taken, a comment is left out.
func TestDanmakuFull(t *testing.T) {
	d := &danmaku{}
	var items []catalog.Danmaku
	for i := range testStyle.lanes(200) + 1 {
		items = append(items, catalog.Danmaku{Text: itoa(i), At: at(1)})
	}
	d.add(items)
	d.advance(at(1), 800, 200, testStyle, fixedWidth)
	if last := d.lanes[len(d.lanes)-1]; last != -1 {
		t.Fatalf("lanes = %v", d.lanes)
	}
}

// Seeking gives lanes again from the comments still in flight there, and
// a page out of order is sorted in.
func TestDanmakuSeekAndOrder(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{Text: "late", At: at(30)}})
	d.add([]catalog.Danmaku{{Text: "early", At: at(2)}})
	if d.items[0].Text != "early" {
		t.Fatalf("items = %v", d.items)
	}
	d.advance(at(3), 800, 600, testStyle, fixedWidth)
	if d.next != 1 || d.lanes[0] != 0 {
		t.Fatalf("next = %d, lanes = %v", d.next, d.lanes)
	}
	d.advance(at(31), 800, 600, testStyle, fixedWidth) // a seek
	if d.next != 2 || d.firstInFlight(at(31)) != 1 || d.lanes[1] != 0 {
		t.Fatalf("after seek: next = %d, lanes = %v", d.next, d.lanes)
	}
}

// The video reporting itself a little back does not give the lanes again.
func TestDanmakuJitter(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{Text: "a", At: at(1)}, {Text: "b", At: at(1)}})
	d.advance(at(2), 800, 600, testStyle, fixedWidth)
	d.advance(at(2)-100*time.Millisecond, 800, 600, testStyle, fixedWidth)
	if d.next != 2 || d.lanes[0] != 0 || d.lanes[1] != 1 {
		t.Fatalf("next = %d, lanes = %v", d.next, d.lanes)
	}
}

// After seeking back, playing on does not give the lanes again each frame.
func TestDanmakuSeekBack(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{Text: "a", At: at(1)}, {Text: "b", At: at(1)}})
	d.advance(at(20), 800, 600, testStyle, fixedWidth)
	d.advance(at(1.5), 800, 600, testStyle, fixedWidth)
	d.lanes[1] = 5 // a reset would give it again
	d.advance(at(1.6), 800, 600, testStyle, fixedWidth)
	if d.lanes[1] != 5 {
		t.Fatalf("lanes given again: %v", d.lanes)
	}
}

// The same comment twice is taken once; the same words while they are
// still in flight count towards the first, shown as ×n, and fly again later.
func TestDanmakuRepeats(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{ID: "1", Text: "+1", At: at(1)}, {ID: "2", Text: "+1", At: at(5)}, {ID: "3", Text: "hi", At: at(5)}})
	d.add([]catalog.Danmaku{{ID: "1", Text: "+1", At: at(1)}, {ID: "4", Text: "+1", At: at(20)}, {ID: "5", Text: "+1", At: at(3)}})
	var got []string
	for i, it := range d.items {
		got = append(got, fmt.Sprintf("%s×%d", it.ID, d.counts[i]))
	}
	if want := "[1×3 3×1 4×1]"; fmt.Sprint(got) != want {
		t.Fatalf("items = %v, want %s", got, want)
	}
	if s := d.spans(0, false); len(s) != 2 || s[1].Text != " ×3" {
		t.Fatalf("spans = %+v", s)
	}
	if s := d.spans(1, false); len(s) != 1 {
		t.Fatalf("spans = %+v", s)
	}
}

// The default settings are the middle steps, and give the test style.
func TestDanmakuDefaults(t *testing.T) {
	app := &App{lib: library.Load(t.TempDir())}
	defer app.lib.Close()
	if !app.lib.DanmakuDefault() || app.danmakuStyle() != testStyle {
		t.Fatalf("style = %+v", app.danmakuStyle())
	}
	app.lib.DanmakuSpeed, app.lib.DanmakuArea, app.lib.DanmakuSize, app.lib.DanmakuOpacity = 99, -1, 4, 0
	if st := app.danmakuStyle(); st.flight != 4500*time.Millisecond || st.area != 0 || st.size != 24 || st.opacity != 0.1 {
		t.Fatalf("style out of range = %+v", st)
	}
}

// The area sets how many lanes a stage has; a fixed number never more than fit.
func TestDanmakuArea(t *testing.T) {
	st := testStyle
	room := float32(600 - danmakuTop - danmakuBottom)
	for area, want := range []int{2, int(room * 0.25 / st.lane()), int(room * 0.5 / st.lane()), int(room * 0.75 / st.lane()), int(room / st.lane())} {
		st.area = area
		if got := st.lanes(600); got != want {
			t.Errorf("area %s: %d lanes, want %d", danmakuAreas[area].label, got, want)
		}
	}
	st.area = 0
	if got := st.lanes(danmakuTop + danmakuBottom + 20); got != 1 {
		t.Errorf("2 lanes on a short stage: %d", got)
	}
}

// Changing the speed or size gives the lanes again; a new size measures
// the comments again.
func TestDanmakuRestyle(t *testing.T) {
	d := &danmaku{}
	d.add([]catalog.Danmaku{{Text: "a", At: at(1)}, {Text: "b", At: at(1)}})
	d.advance(at(2), 800, 600, testStyle, fixedWidth)
	big := testStyle
	big.size = 24
	measured := 0
	d.advance(at(2), 800, 600, big, func([]ui.Span) float32 { measured++; return 140 })
	if measured != 2 || d.widths[0] != 140 {
		t.Fatalf("measured %d, widths %v", measured, d.widths)
	}
	if s := d.spans(0, false); s[0].Size != 24 {
		t.Fatalf("size = %v", s[0].Size)
	}
	faint := big
	faint.opacity = 0.5
	d.advance(at(2), 800, 600, faint, fixedWidth)
	if d.next != 2 || d.lanes[0] != 0 || d.spans(0, false)[0].Color.A >= 200 {
		t.Fatalf("opacity relaid lanes or did not paint: next %d lanes %v", d.next, d.lanes)
	}
}
