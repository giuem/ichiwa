package ui

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/source"
)

// danmakuStep is a step of a danmaku setting: what it says, and what it
// sets.
type danmakuStep struct {
	label  string
	flight time.Duration // the time a comment takes to cross the picture
	lanes  int           // a number of lanes, or
	share  float32       // a part of the stage
	size   float32       // of the text
}

// The steps of the danmaku settings, whose middle ones are the defaults. A
// comment flies in the episode's time: faster as the video plays faster,
// and not at all as it pauses.
var (
	danmakuSpeeds = []danmakuStep{{label: "很慢", flight: 13 * time.Second}, {label: "较慢", flight: 10 * time.Second},
		{label: "适中", flight: 8 * time.Second}, {label: "较快", flight: 6 * time.Second}, {label: "很快", flight: 4500 * time.Millisecond}}
	danmakuAreas = []danmakuStep{{label: "2行", lanes: 2}, {label: "1/4屏", share: 0.25}, {label: "半屏", share: 0.5},
		{label: "3/4屏", share: 0.75}, {label: "满屏", share: 1}}
	danmakuSizes = []danmakuStep{{label: "很小", size: 13}, {label: "较小", size: 15}, {label: "适中", size: 17},
		{label: "较大", size: 20}, {label: "很大", size: 24}}
)

// How the comments are laid out: the room between two comments in a lane,
// how far below the top of the stage the lanes start and above its bottom
// they end, which the controls take, and how long a comment may be.
const (
	danmakuGap      = 28
	danmakuTop      = 56
	danmakuBottom   = 96
	danmakuMaxRunes = 40
)

// danmakuStyle is how the comments show, from the danmaku settings.
type danmakuStyle struct {
	flight  time.Duration
	size    float32
	area    int // into danmakuAreas
	opacity float32
}

// lane is the height of a lane.
func (s danmakuStyle) lane() float32 { return s.size * 1.55 }

// lanes is how many lanes a stage this tall has.
func (s danmakuStyle) lanes(height float32) int {
	room := max(height-danmakuTop-danmakuBottom, 0)
	a := danmakuAreas[s.area]
	if a.share == 0 {
		return min(a.lanes, max(int(room/s.lane()), 1))
	}
	return max(int(room*a.share/s.lane()), 1)
}

// danmakuStyle is the style the settings ask for.
func (app *App) danmakuStyle() danmakuStyle {
	lib := app.Lib
	return danmakuStyle{
		flight:  danmakuSpeeds[clampStep(lib.DanmakuSpeed, len(danmakuSpeeds))].flight,
		size:    danmakuSizes[clampStep(lib.DanmakuSize, len(danmakuSizes))].size,
		area:    clampStep(lib.DanmakuArea, len(danmakuAreas)),
		opacity: float32(min(max(lib.DanmakuOpacity, 0.1), 1)),
	}
}

func clampStep(i, n int) int { return min(max(i, 0), n-1) }

// danmakuCount is the color of how many sent a comment.
var danmakuCount = ui.RGB(255, 214, 102)

// danmakuMerge is how close in time the same words count as one comment.
const danmakuMerge = 8 * time.Second

// danmakuJitter is how far back the video may report itself without that
// counting as a seek.
const danmakuJitter = 250 * time.Millisecond

type danmakuItem struct {
	ID   string
	Text string
	At   time.Duration
}

// danmaku holds the comments of an episode and which lane each flies in.
// A comment takes the first lane in which it neither starts on the one
// before nor catches up with it, as the official player does, and is left
// out when no lane has room. Lanes are given as the video reaches each
// comment, so that seeking or resizing just gives them again from there.
type danmaku struct {
	seen   map[string]bool // the comments taken, by ID
	Items  []danmakuItem   // by when they appear
	counts []int           // how many viewers sent each
	widths []float32       // of their text, measured as they get a lane
	lanes  []int           // the lane of each, -1 for none
	next   int             // the first not given a lane yet
	tails  []int           // the last comment given each lane, -1 for none
	width  float32         // of the stage the lanes are for
	style  danmakuStyle    // the lanes are for
	last   time.Duration   // where the video was when lanes were last given
}

// add takes a page of comments in. A comment already taken is left out,
// and one saying what one taken says while that one is still in flight
// counts towards it rather than flies: many viewers send the same words,
// and a lane each for them fills the picture.
func (d *danmaku) add(raw []source.Danmaku) {
	if len(raw) == 0 {
		return
	}
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	n := len(d.Items)
	for _, r := range raw {
		if r.ID != "" && d.seen[r.ID] {
			continue
		}
		d.seen[r.ID] = true
		it := danmakuItem{
			ID:   r.ID,
			Text: r.Text,
			At:   time.Duration(r.Time * float64(time.Second)),
		}
		if utf8.RuneCountInString(it.Text) > danmakuMaxRunes {
			runes := []rune(it.Text)
			it.Text = string(runes[:danmakuMaxRunes-1]) + "…"
		}
		if k := d.repeats(it); k >= 0 {
			d.counts[k]++
			if k >= d.next { // measured again as it gets a lane
				d.widths[k] = 0
			}
			continue
		}
		d.Items = append(d.Items, it)
		d.counts = append(d.counts, 1)
		d.widths = append(d.widths, 0)
		d.lanes = append(d.lanes, -1)
	}
	if len(d.Items) == n {
		return
	}
	byTime := func(i, j int) bool { return d.Items[i].At < d.Items[j].At }
	if sort.SliceIsSorted(d.Items, byTime) {
		return
	}
	// Out of order: sort them, and give the lanes again.
	idx := make([]int, len(d.Items))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(d.Items[a].At, d.Items[b].At) })
	items, counts, widths := make([]danmakuItem, len(idx)), make([]int, len(idx)), make([]float32, len(idx))
	for k, i := range idx {
		items[k], counts[k], widths[k] = d.Items[i], d.counts[i], d.widths[i]
	}
	d.Items, d.counts, d.widths = items, counts, widths
	d.width = 0 // gives the lanes again
}

// repeats is the comment taken that says what it says near the same
// time, or -1.
func (d *danmaku) repeats(it danmakuItem) int {
	for k, taken := range d.Items {
		if taken.Text == it.Text && (it.At-taken.At).Abs() < danmakuMerge {
			return k
		}
	}
	return -1
}

// advance gives lanes to the comments up to pos, on a stage of this size,
// in style st. measure gives the width of a comment as it is drawn.
func (d *danmaku) advance(pos time.Duration, width, height float32, st danmakuStyle, measure func([]ui.Span) float32) {
	old := d.style
	d.style = st // the opacity just paints
	if st.size != old.size {
		clear(d.widths) // measured again
	}
	lanes := st.lanes(height)
	if width != d.width || lanes != len(d.tails) || st.flight != old.flight || st.size != old.size ||
		pos < d.last-danmakuJitter || pos > d.last+2*time.Second {
		d.reset(pos, width, lanes)
		d.last = pos
	}
	d.last = max(d.last, pos)
	for ; d.next < len(d.Items) && d.Items[d.next].At <= pos; d.next++ {
		d.place(d.next, measure)
	}
}

// reset forgets the lanes given, to give them again from the comments
// still in flight at pos.
func (d *danmaku) reset(pos time.Duration, width float32, lanes int) {
	d.width = width
	d.tails = make([]int, lanes)
	for i := range d.tails {
		d.tails[i] = -1
	}
	d.next = d.firstInFlight(pos)
	for i := d.next; i < len(d.lanes); i++ {
		d.lanes[i] = -1
	}
}

// firstInFlight is the first comment that has not crossed the stage by pos.
func (d *danmaku) firstInFlight(pos time.Duration) int {
	return sort.Search(len(d.Items), func(i int) bool { return d.Items[i].At > pos-d.style.flight })
}

// speed is how fast a comment this wide crosses the stage, in DIPs a second.
func (d *danmaku) speed(w float32) float32 { return (d.width + w) / float32(d.style.flight.Seconds()) }

func (d *danmaku) place(i int, measure func([]ui.Span) float32) {
	if d.widths[i] == 0 {
		d.widths[i] = measure(d.spans(i, false))
	}
	d.lanes[i] = -1
	for l, j := range d.tails {
		if j < 0 || d.follows(j, i) {
			d.lanes[i], d.tails[l] = l, i
			return
		}
	}
}

// follows tells comment i can fly in the lane after comment j: j has come
// in whole, with room behind it, as i comes in, and i, if faster, has not
// caught up with j as j leaves.
func (d *danmaku) follows(j, i int) bool {
	dt := float32((d.Items[i].At - d.Items[j].At).Seconds())
	flight := float32(d.style.flight.Seconds())
	if dt >= flight {
		return true
	}
	vj, vi := d.speed(d.widths[j]), d.speed(d.widths[i])
	if dt*vj < d.widths[j]+danmakuGap {
		return false
	}
	return d.width-(flight-dt)*vi >= danmakuGap
}

// draw paints the comments in flight at pos over the stage r.
func (d *danmaku) draw(p *ui.Painter, r ui.Rect, pos time.Duration) {
	flight := d.style.flight.Seconds()
	for i := d.firstInFlight(pos); i < d.next; i++ {
		lane := d.lanes[i]
		if lane < 0 {
			continue
		}
		w := d.widths[i]
		x := r.X + d.width - float32((pos-d.Items[i].At).Seconds()/flight)*(d.width+w)
		y := r.Y + danmakuTop + float32(lane)*d.style.lane()
		p.RichText(x+1, y+1, 0, d.spans(i, true)...)
		p.RichText(x, y, 0, d.spans(i, false)...)
	}
}

// spans are comment i as it is drawn, or its shadow: its text, and how
// many sent it when more than one did.
func (d *danmaku) spans(i int, shadow bool) []ui.Span {
	st := d.style
	text, count := white.Alpha(0.92*st.opacity), danmakuCount.Alpha(st.opacity)
	if shadow {
		text = ui.RGBA(0, 0, 0, 0.6*st.opacity)
		count = text
	}
	out := []ui.Span{{Text: d.Items[i].Text, Size: st.size, Weight: 600, Color: text}}
	if n := d.counts[i]; n > 1 {
		out = append(out, ui.Span{Text: fmt.Sprintf(" ×%d", n), Size: st.size, Weight: 700, Color: count})
	}
	return out
}

// danmakuLayer flies the comments over the picture.
func (app *App) danmakuLayer(c *ui.Context, w *Watching) {
	feed := w.DM
	if !w.Has(source.CapDanmaku) || !app.Lib.Danmaku || feed == nil || w.Player == nil || w.Err != "" || w.Halt || w.Ended {
		return
	}
	u := uiOf(w)
	if u.dmFor != feed {
		u.dm, u.dmFor, u.dmTaken = &danmaku{}, feed, 0
	}
	d := u.dm
	if len(feed.Items) > u.dmTaken {
		d.add(feed.Items[u.dmTaken:])
		u.dmTaken = len(feed.Items)
	}
	p := w.Player
	st := app.danmakuStyle()
	ui.Box(c.Key("danmaku")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Clip().PassThrough().AccessHidden(true).Children(func() {
		ui.Box(c).Fill().PassThrough().Draw(func(pt *ui.Painter, r ui.Rect) {
			pos := p.Position()
			d.advance(pos, r.W, r.H, st, func(spans []ui.Span) float32 {
				width, _ := pt.MeasureText(0, spans...)
				return width
			})
			d.draw(pt, r, pos)
			if p.Playing() {
				pt.AnimationFrameRate(60)
			}
		})
	})
}

// danmakuSettings is the panel of the danmaku settings, which opens from
// anchor: how opaque the comments are, how fast they fly, how much of the
// picture they take and how large their text is.
func (app *App) danmakuSettings(c *ui.Context, w *Watching, anchor ui.Element) {
	lib := app.Lib
	ui.PopoverBase(c, anchor, &uiOf(w).dmOpen, func(panel ui.Element) {
		popPanel(panel, anchor).Width(300).Padding(14, 18, 18).Gap(16).Radius(14).Label("弹幕设置")
		panel.Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "弹幕设置").FontSize(14).Bold().TextColor(white).Grow(1).Role(ui.RoleHeading).Level(2)
				reset := ui.ButtonBase(c.Key("dm-reset")).Height(24).PaddingX(8).Radius(6).Center().Label("恢复默认").
					Disabled(lib.DanmakuDefault())
				pressable(reset, ui.Transparent, white.Alpha(0.12), 0.95)
				reset.Children(func() { ui.Text(c, "恢复默认").FontSize(12.5).TextColor(white.Alpha(0.75)) })
				if reset.Clicked() {
					lib.ResetDanmaku()
				}
			})
			op := min(max(lib.DanmakuOpacity, 0.1), 1)
			if settingSlider(c.Key("dm-opacity"), "不透明度", fmt.Sprintf("%d%%", int(op*100+0.5)), &op, 0.1, 1, 0.05) {
				lib.DanmakuOpacity = op
				lib.MarkDirty()
			}
			stepSetting(c.Key("dm-speed"), lib, "移动速度", &lib.DanmakuSpeed, danmakuSpeeds)
			stepSetting(c.Key("dm-area"), lib, "显示区域", &lib.DanmakuArea, danmakuAreas)
			stepSetting(c.Key("dm-size"), lib, "字体大小", &lib.DanmakuSize, danmakuSizes)
		})
	})
}

// stepSetting is a setting of steps, *value the one chosen.
func stepSetting(c *ui.Context, lib *library.Library, name string, value *int, steps []danmakuStep) {
	i := clampStep(*value, len(steps))
	v := float64(i)
	if settingSlider(c, name, steps[i].label, &v, 0, float64(len(steps)-1), 1) {
		*value = clampStep(int(v+0.5), len(steps))
		lib.MarkDirty()
	}
}

// settingSlider is a setting's name and value over a slider, with a dot
// at each step when it has a few, and tells whether it changed.
func settingSlider(c *ui.Context, name, value string, v *float64, lo, hi, step float64) bool {
	changed := false
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(white).Grow(1)
			ui.Text(c, value).FontSize(12.5).TextColor(white.Alpha(0.55))
		})
		s := ui.SliderBase(c.Key("slider"), v, lo, hi).Step(step).Height(22).Label(name).Value(value)
		changed = s.Changed()
		hot := s.AnimateTo("hot", boolf(s.Hovered() || s.Dragging()), ui.Smooth)
		at := float32((*v - lo) / (hi - lo))
		steps := int((hi-lo)/step + 0.5)
		s.Draw(func(pt *ui.Painter, r ui.Rect) {
			const knob = 16
			x0, w := r.X+knob/2, r.W-knob
			y := r.Y + r.H/2
			pt.Fill(ui.Rect{X: r.X, Y: y - 2, W: r.W, H: 4}, white.Alpha(0.18), 2)
			pt.Fill(ui.Rect{X: r.X, Y: y - 2, W: knob/2 + w*at, H: 4}, white.Alpha(0.85), 2)
			if steps <= 8 {
				for k := 0; k <= steps; k++ {
					dx := x0 + w*float32(k)/float32(steps)
					pt.Fill(ui.Rect{X: dx - 2, Y: y - 2, W: 4, H: 4}, ui.RGBA(0, 0, 0, 0.35), 2)
				}
			}
			k := knob + 2*hot
			pt.Fill(ui.Rect{X: x0 + w*at - k/2, Y: y - k/2, W: k, H: k}, white, k/2)
		})
	})
	return changed
}
