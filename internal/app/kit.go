package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// palette holds the app's colors, for the light and the dark appearance.
type palette struct {
	dark     bool
	bg       ui.Color // the content's background
	sidebar  ui.Color // under the sidebar where no material shows
	surface  ui.Color // cards, chips, cells
	text     ui.Color
	muted    ui.Color
	faint    ui.Color
	hairline ui.Color
	hover    ui.Color
	press    ui.Color
	accent   ui.Color
	onAccent ui.Color
	shade    ui.Color // the shadow under posters
}

var (
	white = ui.RGB(255, 255, 255)
	black = ui.RGB(0, 0, 0)
)

func lightPalette() palette {
	return palette{
		bg:       ui.Hex("#ffffff"),
		sidebar:  ui.Hex("#f5f5f7"),
		surface:  ui.Hex("#f2f2f5"),
		text:     ui.Hex("#1d1d1f"),
		muted:    ui.RGBA(60, 60, 67, 0.62),
		faint:    ui.RGBA(60, 60, 67, 0.3),
		hairline: ui.RGBA(0, 0, 0, 0.07),
		hover:    ui.RGBA(0, 0, 0, 0.045),
		press:    ui.RGBA(0, 0, 0, 0.08),
		accent:   ui.Oklch(0.62, 0.22, 18),
		onAccent: white,
		shade:    ui.RGBA(0, 0, 0, 0.16),
	}
}

func darkPalette() palette {
	return palette{
		dark:     true,
		bg:       ui.Hex("#141416"),
		sidebar:  ui.Hex("#1c1c1f"),
		surface:  ui.Hex("#202024"),
		text:     ui.Hex("#f5f5f7"),
		muted:    ui.RGBA(235, 235, 245, 0.58),
		faint:    ui.RGBA(235, 235, 245, 0.3),
		hairline: ui.RGBA(255, 255, 255, 0.08),
		hover:    ui.RGBA(255, 255, 255, 0.06),
		press:    ui.RGBA(255, 255, 255, 0.1),
		accent:   ui.Oklch(0.66, 0.21, 18),
		onAccent: white,
		shade:    ui.RGBA(0, 0, 0, 0.45),
	}
}

// contrasted is p for a desktop asking for more contrast (Increase Contrast):
// secondary text and lines stand out clearly on the background.
func (p palette) contrasted() palette {
	p.muted = p.text.Alpha(0.85)
	p.faint = p.text.Alpha(0.7)
	p.hairline = p.text.Alpha(0.35)
	return p
}

// applyPalette makes p the theme's colors, so MyGo's own widgets (the
// search field, the slider, toasts) match the app.
func applyPalette(c *ui.Context, p palette) {
	t := *c.Theme()
	t.Dark = p.dark
	t.Background, t.Surface = p.bg, p.surface
	t.SurfaceHover, t.SurfacePressed = p.surface.Mix(p.text, 0.06), p.surface.Mix(p.text, 0.1)
	t.Text, t.TextMuted, t.Border = p.text, p.muted, p.hairline
	t.Accent, t.AccentText = p.accent, p.onAccent
	if p.dark {
		t.AccentHover, t.AccentPressed = p.accent.MixOklab(white, 0.15), p.accent.MixOklab(black, 0.15)
	} else {
		t.AccentHover, t.AccentPressed = p.accent.MixOklab(black, 0.1), p.accent.MixOklab(black, 0.2)
	}
	t.Selection = p.accent.Alpha(0.3)
	t.Focus = p.accent.Alpha(0.6)
	if c.Preferences().HighContrast {
		t.Focus = p.accent
	}
	t.Radius = 8
	c.SetTheme(&t)
}

var quick = ui.Animation{Duration: 120 * time.Millisecond}

// pressable gives b the hover fill and the press give of the app's buttons.
func pressable(b ui.Element, rest, hover ui.Color, give float32) ui.Element {
	bg := rest
	if b.Hovered() {
		bg = hover
	}
	b.Background(b.AnimateColor("bg", bg, quick))
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = give
	}
	return b.AnimatePose(pose, ui.Snappy)
}

// tooltip is Element.Tooltip for a tip that repeats the element's name,
// which Tooltip would have screen readers read a second time.
func tooltip(c *ui.Context, anchor ui.Element, s string) {
	t := c.Theme()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.Padding(5, 8).Radius(5).Background(t.Text).TextColor(t.Background).FontSize(t.FontSize-1).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2)).
			AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 6, 0)
		tip.Children(func() { ui.Text(c, s) })
	})
}

// roundButton is a borderless round button, which a soft circle marks
// under the pointer and which gives a little as it is pressed. label may end in the key that
// presses it, as "返回 (Esc)": the tooltip shows the key, and screen readers
// read only the name, being told the key with KeyShortcuts.
func roundButton(c *ui.Context, size float32, color ui.Color, label string) ui.Element {
	name, _, _ := strings.Cut(label, " (")
	b := ui.ButtonBase(c).Size(size+16, size+16).Center().Radius(999).Label(name).TextColor(color)
	if label != "" {
		tooltip(c, b, label)
	}
	return pressable(b, ui.Transparent, ui.RGBA(128, 128, 128, 0.16), 0.88)
}

// iconButton is a roundButton showing a Lucide icon.
func iconButton(c *ui.Context, icon *ui.SVG, size float32, color ui.Color, label string) ui.Element {
	b := roundButton(c, size, color, label)
	b.Children(func() { ui.Icon(c, icon).Size(size, size) })
	return b
}

// symbolButton is a roundButton showing an SF Symbol.
func symbolButton(c *ui.Context, symbol string, size float32, color ui.Color, label string) ui.Element {
	b := roundButton(c, size, color, label)
	b.Children(func() { sfsymbols.Symbol(c, symbol).Size(size).Color(color) })
	return b
}

// pillButton is a rounded button with an optional icon and a label.
func pillButton(c *ui.Context, icon *ui.SVG, label string, bg, fg ui.Color) ui.Element {
	b := ui.ButtonBase(c).Height(34).PaddingX(16).Gap(7).Radius(999).AlignItems(ui.Center).TextColor(fg)
	pressable(b, bg, bg.Mix(fg, 0.12), 0.96)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(600).SingleLine()
	})
	return b
}

// primary is the accent pill; secondary the quiet one.
func (app *App) primary(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return pillButton(c, icon, label, app.pal.accent, app.pal.onAccent)
}

func (app *App) secondary(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return pillButton(c, icon, label, app.pal.surface, app.pal.text)
}

// chip is a small pill choosing one of several options.
func (app *App) chip(c *ui.Context, label string, selected bool) ui.Element {
	p := app.pal
	// The ring goes inside: chips sit at the edges of scrolling rows, which clip.
	b := ui.ButtonBase(c).Height(30).PaddingX(14).Radius(999).Center().FocusRingOffset(-2)
	rest, fg := p.surface, p.muted
	if selected {
		rest, fg = p.text, p.bg
	}
	pressable(b, rest, rest.Mix(p.text, 0.08), 0.95)
	b.Children(func() {
		t := ui.Text(c, label).FontSize(12.5).SingleLine().TextColor(b.AnimateColor("fg", fg, quick))
		if selected {
			t.FontWeight(600)
		}
	})
	return b
}

// radio makes b one of a group's choices for assistive technology, inside
// a choices row.
func radio(b ui.Element, on bool) ui.Element { return b.Role(ui.RoleRadio).Checked(on) }

// choices is a row of radio buttons, one stop of Tab, the arrows moving
// among them.
func choices(c *ui.Context, label string) ui.Element {
	return ui.Row(c).Role(ui.RoleRadioGroup).Label(label).FocusGroup(ui.Horizontal)
}

// tab is a text tab with an accent bar under the chosen one.
func (app *App) tab(c *ui.Context, label string, selected bool) ui.Element {
	p := app.pal
	b := ui.ButtonBase(c).PaddingY(4).Role(ui.RoleTab).Label(label).AccessSelected(selected)
	on := b.AnimateTo("on", boolf(selected), ui.Smooth)
	color := p.muted
	if selected || b.Hovered() {
		color = p.text
	}
	b.Children(func() {
		ui.Column(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(15).FontWeight(600).TextColor(b.AnimateColor("fg", color, quick)).SingleLine()
			ui.Box(c).Height(3).Width(18 * on).Radius(2).Background(p.accent).Opacity(on)
		})
	})
	return b
}

// ---- Skeletons ----

// shimmer is how the blocks of a skeleton look: gray blocks with a lighter
// band sweeping across them, in step across the window.
type shimmer struct {
	base, sheen ui.Color
	span        float32
	still       bool
}

const (
	shimmerPeriod = 1600 * time.Millisecond
	shimmerSweep  = 1100 * time.Millisecond
	shimmerBand   = 260
)

func (app *App) shimmer(c *ui.Context) shimmer {
	s := shimmer{base: ui.RGBA(0, 0, 0, 0.06), sheen: ui.RGBA(0, 0, 0, 0.02)}
	if app.pal.dark {
		s.base, s.sheen = white.Alpha(0.07), white.Alpha(0.13)
	}
	s.span, _ = c.Size()
	s.still = c.Preferences().ReduceMotion
	return s
}

func (s shimmer) paint(p *ui.Painter, r ui.Rect, radius float32) {
	if s.still || r.W <= 0 {
		p.Fill(r, s.base, radius)
		return
	}
	at := time.Duration(p.Now().UnixNano() % int64(shimmerPeriod))
	if at >= shimmerSweep {
		p.Fill(r, s.base, radius)
		p.After(shimmerPeriod - at)
		return
	}
	p.AnimationFrameRate(60)
	t := float32(at) / float32(shimmerSweep)
	x := -shimmerBand/2 + t*(s.span+shimmerBand)
	if x+shimmerBand/2 < r.X || x-shimmerBand/2 > r.X+r.W {
		p.Fill(r, s.base, radius)
		return
	}
	along := func(x float32) float32 { return (x - r.X) / r.W }
	p.FillGradient(r, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
		{Color: s.base, At: along(x - shimmerBand/2)},
		{Color: s.sheen, At: along(x)},
		{Color: s.base, At: along(x + shimmerBand/2)},
	}}, radius)
}

// bone is a block of the skeleton, as big as it is laid out.
func (s shimmer) bone(c *ui.Context, radius float32) ui.Element {
	return ui.Box(c).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) { s.paint(p, r, radius) })
}

// ---- Artwork ----

var artFade = ui.Animation{Duration: 320 * time.Millisecond}

// artIn returns how far bmp has faded in on el: over a moment once it
// loads while el shows, and at once if it was ready as el first showed.
func artIn(el ui.Element, bmp *ui.Bitmap) float32 {
	if bmp == nil {
		return el.Jump("art", 0)
	}
	return el.AnimateTo("art", 1, artFade)
}

func boolf(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
